package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/redis/go-redis/v9"

	"github.com/omni/bugtracker/internal/auth"
)

// Bucket names a class of request with its own budget. Reads are cheap and frequent,
// writes are neither, search runs full-text queries, and inbound webhooks arrive from
// unauthenticated callers we can only identify by address.
const (
	BucketRead    = "read"
	BucketWrite   = "write"
	BucketSearch  = "search"
	BucketInbound = "inbound"
)

// Budgets is the per-window request allowance for each bucket. A budget of 0 disables
// limiting for that bucket.
type Budgets struct {
	Window  time.Duration
	Read    int
	Write   int
	Search  int
	Inbound int
}

// Limit returns the allowance for a bucket.
func (b Budgets) Limit(bucket string) int {
	switch bucket {
	case BucketWrite:
		return b.Write
	case BucketSearch:
		return b.Search
	case BucketInbound:
		return b.Inbound
	default:
		return b.Read
	}
}

// Verdict is one limiter decision.
type Verdict struct {
	Allowed    bool
	Remaining  int
	RetryAfter time.Duration
}

// Limiter counts a request against a key's budget. Implementations must be safe for
// concurrent use.
type Limiter interface {
	Allow(ctx context.Context, key string, limit int, window time.Duration) (Verdict, error)
}

// Observer records limiter outcomes ("limited" or "error") for metrics. Optional.
type Observer func(bucket, outcome string)

// RateLimit rejects requests that exceed their bucket's budget, keyed by the calling
// principal (API token, then user, then source address).
//
// It fails OPEN: if the limiter errors — Redis down, script failure — the request is
// served. A cache outage degrading into a full API outage would be a worse failure than
// the one rate limiting exists to prevent. Failures are logged and counted.
func RateLimit(l Limiter, b Budgets, logger *slog.Logger, obs Observer) func(http.Handler) http.Handler {
	return limitBy(l, b, logger, obs, bucketFor, principalKey)
}

// RateLimitInbound limits the unauthenticated integration webhooks by source address.
// These endpoints run before Auth, so there is no principal to key on.
func RateLimitInbound(l Limiter, b Budgets, logger *slog.Logger, obs Observer) func(http.Handler) http.Handler {
	return limitBy(l, b, logger, obs,
		func(*http.Request) string { return BucketInbound },
		func(r *http.Request) string { return "ip:" + clientIP(r) })
}

func limitBy(
	l Limiter, b Budgets, logger *slog.Logger, obs Observer,
	bucketOf func(*http.Request) string, keyOf func(*http.Request) string,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if l == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bucket := bucketOf(r)
			limit := b.Limit(bucket)
			if limit <= 0 { // bucket disabled
				next.ServeHTTP(w, r)
				return
			}

			v, err := l.Allow(r.Context(), "ratelimit:"+bucket+":"+keyOf(r), limit, b.Window)
			if err != nil {
				report(obs, bucket, "error")
				logger.Warn("rate limiter unavailable — allowing request",
					"err", err, "bucket", bucket,
					"request_id", middleware.GetReqID(r.Context()))
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("RateLimit-Limit", strconv.Itoa(limit))
			w.Header().Set("RateLimit-Remaining", strconv.Itoa(v.Remaining))
			w.Header().Set("RateLimit-Reset", strconv.Itoa(seconds(resetIn(v, b.Window))))

			if !v.Allowed {
				report(obs, bucket, "limited")
				w.Header().Set("Retry-After", strconv.Itoa(seconds(v.RetryAfter)))
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"title":"too many requests","status":429,"detail":"` +
					bucket + ` budget of ` + strconv.Itoa(limit) + ` per ` + b.Window.String() +
					` exhausted — retry in ` + strconv.Itoa(seconds(v.RetryAfter)) + `s"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// resetIn is when the budget next frees up: the caller's retry hint when limited, and
// one full window when not (the oldest entry cannot be older than that).
func resetIn(v Verdict, window time.Duration) time.Duration {
	if !v.Allowed && v.RetryAfter > 0 {
		return v.RetryAfter
	}
	return window
}

// seconds rounds up, so a 200ms wait is reported as 1s rather than 0 — a client that
// retries immediately on "0" would just be limited again.
func seconds(d time.Duration) int {
	if d <= 0 {
		return 1
	}
	return int((d + time.Second - 1) / time.Second)
}

func report(obs Observer, bucket, outcome string) {
	if obs != nil {
		obs(bucket, outcome)
	}
}

// bucketFor classifies a request. Search runs ranked full-text queries and is an order
// of magnitude more expensive than the reads it otherwise looks like.
func bucketFor(r *http.Request) string {
	if strings.HasSuffix(r.URL.Path, "/search") {
		return BucketSearch
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return BucketRead
	default:
		return BucketWrite
	}
}

// principalKey identifies who to charge. A token gets its own budget so one runaway
// script cannot exhaust its owner's browser session, and vice versa.
func principalKey(r *http.Request) string {
	p := auth.FromContext(r.Context())
	switch {
	case p == nil:
		return "ip:" + clientIP(r)
	case p.ViaToken && p.TokenID != "":
		return "t:" + p.TokenID
	case p.UserID != "":
		return "u:" + p.UserID
	default:
		return "ip:" + clientIP(r)
	}
}

// clientIP prefers chi's RealIP result (already applied as an earlier middleware) and
// strips the port so one client is one key.
func clientIP(r *http.Request) string {
	addr := r.RemoteAddr
	if i := strings.LastIndex(addr, ":"); i > strings.LastIndex(addr, "]") {
		addr = addr[:i]
	}
	return strings.Trim(addr, "[]")
}

// slidingWindow keeps one sorted set per key holding the timestamp of each request in
// the window. Expired entries are dropped on read, so the count is exact rather than
// the bucket-boundary approximation a plain INCR gives — a client cannot get 2x the
// budget by straddling the boundary.
//
// KEYS[1] = the key. ARGV = now (ms), window (ms), limit, unique member.
// Returns {allowed, remaining, retryAfterMillis}.
var slidingWindow = redis.NewScript(`
local key    = KEYS[1]
local now    = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local limit  = tonumber(ARGV[3])
local member = ARGV[4]

redis.call('ZREMRANGEBYSCORE', key, 0, now - window)
local used = redis.call('ZCARD', key)
if used < limit then
  redis.call('ZADD', key, now, member)
  redis.call('PEXPIRE', key, window)
  return {1, limit - used - 1, 0}
end

local oldest = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
local retry = window
if oldest[2] then
  retry = (tonumber(oldest[2]) + window) - now
  if retry < 0 then retry = 0 end
end
redis.call('PEXPIRE', key, window)
return {0, 0, retry}
`)

// RedisLimiter is the production Limiter.
type RedisLimiter struct {
	rdb *redis.Client
	seq atomic.Uint64
}

// NewRedisLimiter returns a limiter backed by rdb, or nil if rdb is nil (which makes
// RateLimit a no-op — the API still serves when Redis was never reachable at boot).
func NewRedisLimiter(rdb *redis.Client) *RedisLimiter {
	if rdb == nil {
		return nil
	}
	return &RedisLimiter{rdb: rdb}
}

// Allow implements Limiter.
func (l *RedisLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (Verdict, error) {
	now := time.Now().UnixMilli()
	// Two requests in the same millisecond need distinct members or the second one
	// would overwrite the first's score and be served for free.
	member := strconv.FormatInt(now, 10) + "-" + strconv.FormatUint(l.seq.Add(1), 36)

	res, err := slidingWindow.Run(ctx, l.rdb, []string{key},
		now, window.Milliseconds(), limit, member).Int64Slice()
	if err != nil || len(res) < 3 {
		if err == nil {
			err = redis.Nil
		}
		return Verdict{}, err
	}
	return Verdict{
		Allowed:    res[0] == 1,
		Remaining:  int(res[1]),
		RetryAfter: time.Duration(res[2]) * time.Millisecond,
	}, nil
}
