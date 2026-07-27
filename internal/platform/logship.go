package platform

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/omni/bugtracker/internal/config"
)

// Shipping omni-bugtracker's own logs to Omni-Logging.
//
// The comment on NewLogger has always claimed the JSON output "ships cleanly to
// Omni-Logging"; nothing was actually shipping it. This does, by fanning the same
// slog records that go to stdout into a batched NDJSON POST at /api/v1/ingest.
//
// Three rules shape everything below, in order of importance:
//
//  1. Never block the caller. A log call happens inside request handling and inside
//     job execution; if the log pipeline can stall those, an unreachable log server
//     becomes an outage. The queue is bounded and a full queue drops.
//  2. Never log about logging. Reporting a shipper failure through slog would feed
//     the failure back into the thing that failed — one dropped batch becomes an
//     unbounded loop. Failures go straight to stderr, heavily rate-limited.
//  3. Never replace stdout. Shipping is additive. `docker logs` has to keep working,
//     because it is what you reach for when the log server is the thing that's broken.

const (
	// queueCapacity is roughly a second of very chatty logging. Past that, the
	// server is not keeping up and the useful thing is to shed and say so, not to
	// grow until the process is killed.
	queueCapacity = 2048
	// batchSize / flushInterval trade request count against delivery latency. Logs
	// are not alerts; a second of lag is fine and 2000 one-line POSTs are not.
	batchSize     = 256
	flushInterval = time.Second
	// dropReportInterval rate-limits the stderr complaint. Once a server is down
	// every record drops, and a line per drop would be its own outage.
	dropReportInterval = 30 * time.Second
)

// LogShipper batches slog records and POSTs them to Omni-Logging.
type LogShipper struct {
	endpoint string
	apiKey   string
	client   *http.Client

	queue  chan []byte
	done   chan struct{}
	closed sync.Once

	dropped    atomic.Int64
	lastReport atomic.Int64 // unix nanos
}

// newLogShipper starts the background sender. A nil return means shipping is off.
func newLogShipper(cfg config.LogShip) *LogShipper {
	if !cfg.Enabled || cfg.Endpoint == "" {
		return nil
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	s := &LogShipper{
		endpoint: cfg.Endpoint,
		apiKey:   cfg.APIKey,
		client:   &http.Client{Timeout: timeout},
		queue:    make(chan []byte, queueCapacity),
		done:     make(chan struct{}),
	}
	go s.run()
	return s
}

// Write accepts one NDJSON line from the shipping slog handler.
//
// It is an io.Writer so the encoder can be slog's own JSONHandler: whatever ends up
// in Omni-Logging is byte-for-byte what stdout shows, rather than a second
// serialisation that can drift from it.
func (s *LogShipper) Write(p []byte) (int, error) {
	// The handler reuses its buffer, so the line must be copied before it is queued.
	line := make([]byte, len(bytes.TrimRight(p, "\n")))
	copy(line, bytes.TrimRight(p, "\n"))
	if len(line) == 0 {
		return len(p), nil
	}
	select {
	case s.queue <- line:
	default:
		// Dropped rather than blocked — see rule 1. Reported, never silently.
		s.dropped.Add(1)
		s.reportDrops()
	}
	// Always reports success: a logging call site cannot do anything useful with a
	// shipping error, and propagating one would only produce more logging.
	return len(p), nil
}

func (s *LogShipper) run() {
	defer close(s.done)
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	batch := make([][]byte, 0, batchSize)
	for {
		select {
		case line, ok := <-s.queue:
			if !ok {
				s.send(batch)
				return
			}
			batch = append(batch, line)
			if len(batch) >= batchSize {
				s.send(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				s.send(batch)
				batch = batch[:0]
			}
		}
	}
}

func (s *LogShipper) send(batch [][]byte) {
	if len(batch) == 0 {
		return
	}
	var body bytes.Buffer
	for _, line := range batch {
		body.Write(line)
		body.WriteByte('\n')
	}

	req, err := http.NewRequest(http.MethodPost, s.endpoint, &body)
	if err != nil {
		s.complain("build request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/x-ndjson")
	if s.apiKey != "" {
		req.Header.Set("X-Api-Key", s.apiKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		s.complain("post %d records: %v", len(batch), err)
		return
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode >= 300 {
		// 401 (bad key) and 429 (quota) are the two that actually happen, and both
		// need the status to be diagnosable at all.
		s.complain("post %d records: %s", len(batch), resp.Status)
	}
}

// Close flushes what is queued and stops the sender. Bounded so a hung log server
// cannot hold up process shutdown.
func (s *LogShipper) Close() {
	if s == nil {
		return
	}
	s.closed.Do(func() {
		close(s.queue)
		select {
		case <-s.done:
		case <-time.After(3 * time.Second):
			s.complain("gave up flushing at shutdown")
		}
		if n := s.dropped.Load(); n > 0 {
			s.complain("%d records were dropped over this process's lifetime", n)
		}
	})
}

// reportDrops emits at most one stderr line per dropReportInterval.
func (s *LogShipper) reportDrops() {
	now := time.Now().UnixNano()
	last := s.lastReport.Load()
	if now-last < int64(dropReportInterval) {
		return
	}
	if !s.lastReport.CompareAndSwap(last, now) {
		return // another goroutine is reporting; one line is the point
	}
	s.complain("log queue full, %d records dropped so far", s.dropped.Load())
}

// complain writes to stderr directly. Deliberately not slog — see rule 2.
func (s *LogShipper) complain(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "logship: "+format+"\n", args...)
}

// fanoutHandler writes each record to both handlers.
//
// A slog.Handler rather than a second logger so WithAttrs/WithGroup — and therefore
// the `service` attribute every record carries — apply identically to both sides.
type fanoutHandler struct {
	handlers []slog.Handler
}

func (f fanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range f.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (f fanoutHandler) Handle(ctx context.Context, r slog.Record) error {
	var firstErr error
	for _, h := range f.handlers {
		if !h.Enabled(ctx, r.Level) {
			continue
		}
		// Each handler gets its own clone: Record carries internal state that
		// handlers are allowed to consume, so sharing one across two is not safe.
		if err := h.Handle(ctx, r.Clone()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (f fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, len(f.handlers))
	for i, h := range f.handlers {
		next[i] = h.WithAttrs(attrs)
	}
	return fanoutHandler{handlers: next}
}

func (f fanoutHandler) WithGroup(name string) slog.Handler {
	next := make([]slog.Handler, len(f.handlers))
	for i, h := range f.handlers {
		next[i] = h.WithGroup(name)
	}
	return fanoutHandler{handlers: next}
}
