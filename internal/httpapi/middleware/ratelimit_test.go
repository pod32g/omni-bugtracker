package middleware

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/omni/bugtracker/internal/auth"
)

// fakeLimiter counts calls per key and refuses past a fixed allowance, so the
// middleware's own behaviour can be tested without a Redis.
type fakeLimiter struct {
	seen map[string]int
	err  error
}

func (f *fakeLimiter) Allow(_ context.Context, key string, limit int, _ time.Duration) (Verdict, error) {
	if f.err != nil {
		return Verdict{}, f.err
	}
	if f.seen == nil {
		f.seen = map[string]int{}
	}
	f.seen[key]++
	if f.seen[key] > limit {
		return Verdict{Allowed: false, RetryAfter: 1500 * time.Millisecond}, nil
	}
	return Verdict{Allowed: true, Remaining: limit - f.seen[key]}, nil
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func ok(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }

func withPrincipal(r *http.Request, p *auth.Principal) *http.Request {
	return r.WithContext(auth.WithPrincipal(r.Context(), p))
}

func TestRateLimitAllowsUpToTheBudgetThen429s(t *testing.T) {
	h := RateLimit(&fakeLimiter{}, Budgets{Window: time.Minute, Read: 2}, quietLogger(), nil)(http.HandlerFunc(ok))
	p := &auth.Principal{UserID: "u1"}

	for i := 1; i <= 2; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, withPrincipal(httptest.NewRequest(http.MethodGet, "/api/v1/issues", nil), p))
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: got %d, want 200", i, w.Code)
		}
		if w.Header().Get("RateLimit-Limit") != "2" {
			t.Errorf("RateLimit-Limit = %q, want 2", w.Header().Get("RateLimit-Limit"))
		}
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, withPrincipal(httptest.NewRequest(http.MethodGet, "/api/v1/issues", nil), p))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("third request: got %d, want 429", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("content-type = %q, want problem+json", got)
	}
	// 1.5s must round up: a client honouring "1" retries too early, one honouring
	// "2" is polite, but "0" or "1s" truncation would spin.
	if got := w.Header().Get("Retry-After"); got != "2" {
		t.Errorf("Retry-After = %q, want 2", got)
	}
}

// Reads, writes and search draw on separate budgets — a write flood must not lock the
// caller out of reading.
func TestBucketsAreCountedSeparately(t *testing.T) {
	f := &fakeLimiter{}
	b := Budgets{Window: time.Minute, Read: 1, Write: 1, Search: 1}
	h := RateLimit(f, b, quietLogger(), nil)(http.HandlerFunc(ok))
	p := &auth.Principal{UserID: "u1"}

	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "/api/v1/projects/BUG/issues", nil),
		httptest.NewRequest(http.MethodGet, "/api/v1/issues", nil),
		httptest.NewRequest(http.MethodGet, "/api/v1/search", nil),
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, withPrincipal(req, p))
		if w.Code != http.StatusOK {
			t.Errorf("%s %s: got %d, want 200", req.Method, req.URL.Path, w.Code)
		}
	}
	for _, want := range []string{"ratelimit:read:u:u1", "ratelimit:write:u:u1", "ratelimit:search:u:u1"} {
		if f.seen[want] != 1 {
			t.Errorf("key %q counted %d times, want 1 (keys: %v)", want, f.seen[want], f.seen)
		}
	}
}

// Two tokens belonging to the same user must not share a budget: one runaway script
// should not lock its owner out of the browser.
func TestTokensAreBudgetedSeparatelyFromSessions(t *testing.T) {
	f := &fakeLimiter{}
	h := RateLimit(f, Budgets{Window: time.Minute, Read: 5}, quietLogger(), nil)(http.HandlerFunc(ok))

	for _, p := range []*auth.Principal{
		{UserID: "u1", ViaToken: true, TokenID: "tok-a"},
		{UserID: "u1", ViaToken: true, TokenID: "tok-b"},
		{UserID: "u1"},
	} {
		h.ServeHTTP(httptest.NewRecorder(),
			withPrincipal(httptest.NewRequest(http.MethodGet, "/api/v1/issues", nil), p))
	}
	for _, want := range []string{"ratelimit:read:t:tok-a", "ratelimit:read:t:tok-b", "ratelimit:read:u:u1"} {
		if f.seen[want] != 1 {
			t.Errorf("key %q counted %d times, want 1 (keys: %v)", want, f.seen[want], f.seen)
		}
	}
}

// A Redis outage must degrade to "unlimited", never to "API down".
func TestLimiterErrorFailsOpen(t *testing.T) {
	var outcomes []string
	obs := func(bucket, outcome string) { outcomes = append(outcomes, bucket+":"+outcome) }
	f := &fakeLimiter{err: errors.New("dial tcp: connection refused")}
	h := RateLimit(f, Budgets{Window: time.Minute, Read: 1}, quietLogger(), obs)(http.HandlerFunc(ok))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, withPrincipal(httptest.NewRequest(http.MethodGet, "/api/v1/issues", nil),
		&auth.Principal{UserID: "u1"}))

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want the request served despite the limiter failing", w.Code)
	}
	if len(outcomes) != 1 || outcomes[0] != "read:error" {
		t.Errorf("observed %v, want [read:error]", outcomes)
	}
}

// A zero budget means "not limited", which is what an unconfigured or disabled
// rate_limit block produces. It must not mean "limit of zero, reject everything".
func TestZeroBudgetDisablesTheBucket(t *testing.T) {
	f := &fakeLimiter{}
	h := RateLimit(f, Budgets{}, quietLogger(), nil)(http.HandlerFunc(ok))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, withPrincipal(httptest.NewRequest(http.MethodGet, "/api/v1/issues", nil),
		&auth.Principal{UserID: "u1"}))

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	if len(f.seen) != 0 {
		t.Errorf("limiter consulted for a disabled bucket: %v", f.seen)
	}
}

func TestNilLimiterIsANoOp(t *testing.T) {
	h := RateLimit(nil, Budgets{Window: time.Minute, Read: 1}, quietLogger(), nil)(http.HandlerFunc(ok))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/issues", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 when no limiter is configured", w.Code)
	}
}

// Inbound webhooks run before Auth, so they key on the address.
func TestInboundLimiterKeysOnAddress(t *testing.T) {
	f := &fakeLimiter{}
	h := RateLimitInbound(f, Budgets{Window: time.Minute, Inbound: 5}, quietLogger(), nil)(http.HandlerFunc(ok))

	r := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/git/events", nil)
	r.RemoteAddr = "10.1.2.3:54321"
	h.ServeHTTP(httptest.NewRecorder(), r)

	if f.seen["ratelimit:inbound:ip:10.1.2.3"] != 1 {
		t.Errorf("want the port stripped from the key, got %v", f.seen)
	}
}

func TestClientIPStripsPort(t *testing.T) {
	for _, tc := range []struct{ addr, want string }{
		{"10.1.2.3:54321", "10.1.2.3"},
		{"[2001:db8::1]:443", "2001:db8::1"},
		{"10.1.2.3", "10.1.2.3"},
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = tc.addr
		if got := clientIP(r); got != tc.want {
			t.Errorf("clientIP(%q) = %q, want %q", tc.addr, got, tc.want)
		}
	}
}

func TestSecondsRoundsUp(t *testing.T) {
	for _, tc := range []struct {
		in   time.Duration
		want int
	}{
		{0, 1}, {1 * time.Millisecond, 1}, {time.Second, 1}, {1001 * time.Millisecond, 2}, {90 * time.Second, 90},
	} {
		if got := seconds(tc.in); got != tc.want {
			t.Errorf("seconds(%s) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
