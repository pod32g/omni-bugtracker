package platform

import (
	"bufio"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/omni/bugtracker/internal/config"
)

// collector is a stand-in for Omni-Logging's ingest endpoint.
type collector struct {
	mu      sync.Mutex
	records []map[string]any
	keys    []string
	types   []string
	status  int
	block   chan struct{} // when non-nil, handlers wait on it
}

func (c *collector) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if c.block != nil {
			<-c.block
		}
		c.mu.Lock()
		c.keys = append(c.keys, r.Header.Get("X-Api-Key"))
		c.types = append(c.types, r.Header.Get("Content-Type"))
		c.mu.Unlock()

		scanner := bufio.NewScanner(r.Body)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var rec map[string]any
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			c.mu.Lock()
			c.records = append(c.records, rec)
			c.mu.Unlock()
		}
		if c.status != 0 {
			w.WriteHeader(c.status)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}

func (c *collector) got() []map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]map[string]any(nil), c.records...)
}

func newTestLogger(t *testing.T, srv *httptest.Server, key string) (*LogShipper, func()) {
	t.Helper()
	shipper := newLogShipper(config.LogShip{
		Enabled: true, Endpoint: srv.URL, APIKey: key, Timeout: 2 * time.Second,
	})
	if shipper == nil {
		t.Fatal("shipper should be enabled")
	}
	return shipper, shipper.Close
}

// loggerWithShipper mirrors what NewLogger assembles, with the stdout side discarded
// so the tests stay quiet. It has to stay in step with NewLogger: the point of these
// tests is the shape of what reaches the wire.
func loggerWithShipper(shipper *LogShipper) (*slog.Logger, slog.Handler) {
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	h := slog.Handler(fanoutHandler{handlers: []slog.Handler{
		slog.NewJSONHandler(io.Discard, opts),
		slog.NewJSONHandler(shipper, opts),
	}})
	return slog.New(h).With("service", "omni-bugtracker"), h
}

// The whole feature is worthless if the shipped record is not the record. This asserts
// the wire format Omni-Logging actually parses: `time`, `level`, `msg` and the
// `service` attribute are all aliases its EventFromJSON maps to first-class fields.
func TestShippedRecordCarriesTheFieldsOmniLoggingParses(t *testing.T) {
	c := &collector{}
	srv := httptest.NewServer(c.handler())
	defer srv.Close()

	shipper, closeFn := newTestLogger(t, srv, "test-key")
	logger, _ := loggerWithShipper(shipper)
	logger.Info("issue created", "issue", "BUG-42", "count", 3)
	closeFn()

	got := c.got()
	if len(got) != 1 {
		t.Fatalf("expected one shipped record, got %d", len(got))
	}
	rec := got[0]
	for _, field := range []string{"time", "level", "msg", "service"} {
		if _, ok := rec[field]; !ok {
			t.Errorf("shipped record is missing %q — Omni-Logging maps it to a first-class field: %v", field, rec)
		}
	}
	if rec["msg"] != "issue created" {
		t.Errorf("msg = %v", rec["msg"])
	}
	if rec["service"] != "omni-bugtracker" {
		t.Errorf("service = %v — the attribute must survive the fan-out", rec["service"])
	}
	// Structured attributes have to survive too, or shipping is worse than raw text.
	if rec["issue"] != "BUG-42" {
		t.Errorf("attribute lost: %v", rec)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.keys[0] != "test-key" {
		t.Errorf("X-Api-Key = %q", c.keys[0])
	}
	if c.types[0] != "application/x-ndjson" {
		t.Errorf("Content-Type = %q", c.types[0])
	}
}

// Rule 1: a log call must never wait on the log server. With the collector wedged and
// the queue over-full, logging still has to return promptly — an unreachable log
// server must not become an outage.
func TestLoggingDoesNotBlockOnAnUnresponsiveServer(t *testing.T) {
	c := &collector{block: make(chan struct{})}
	srv := httptest.NewServer(c.handler())
	defer srv.Close()

	shipper := newLogShipper(config.LogShip{Enabled: true, Endpoint: srv.URL, Timeout: time.Second})
	logger, _ := loggerWithShipper(shipper)

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Comfortably more than queueCapacity, so the drop path is exercised.
		for i := 0; i < queueCapacity*2; i++ {
			logger.Info("flood")
		}
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("logging blocked while the collector was unresponsive — a log call must never wait")
	}
	if shipper.dropped.Load() == 0 {
		t.Error("expected drops once the queue filled; a silent unbounded queue is the other failure mode")
	}
	close(c.block)
	shipper.Close()
}

// A non-2xx response must not lose the process. 401 (wrong key) and 429 (quota) are
// the two that actually happen.
func TestRejectedBatchDoesNotPanicOrBlock(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests} {
		c := &collector{status: status}
		srv := httptest.NewServer(c.handler())

		shipper := newLogShipper(config.LogShip{Enabled: true, Endpoint: srv.URL, Timeout: time.Second})
		logger, _ := loggerWithShipper(shipper)
		logger.Info("rejected batch")
		shipper.Close()
		srv.Close()
	}
}

// Disabled shipping returns nil, and a nil shipper must be safe to Close — the
// deferred Close in main runs on every path, including the default one.
func TestDisabledShippingIsNilAndSafe(t *testing.T) {
	if s := newLogShipper(config.LogShip{Enabled: false, Endpoint: "http://x"}); s != nil {
		t.Error("disabled shipping should return nil")
	}
	// An enabled shipper with no endpoint is a misconfiguration, not a reason to
	// start POSTing to the empty string.
	if s := newLogShipper(config.LogShip{Enabled: true, Endpoint: ""}); s != nil {
		t.Error("an empty endpoint should disable shipping")
	}
	var nilShipper *LogShipper
	nilShipper.Close() // must not panic
}

// Close is called from a deferred main and could plausibly be reached twice.
func TestCloseIsIdempotent(t *testing.T) {
	c := &collector{}
	srv := httptest.NewServer(c.handler())
	defer srv.Close()

	shipper := newLogShipper(config.LogShip{Enabled: true, Endpoint: srv.URL, Timeout: time.Second})
	logger, _ := loggerWithShipper(shipper)
	logger.Info("one")
	shipper.Close()
	shipper.Close() // must not panic on a closed channel
}

// Records logged before shutdown must actually arrive: the flush interval is a
// second, so without a flush-on-close everything logged in the last second is lost —
// which is exactly the window a crash investigation cares about.
func TestCloseFlushesQueuedRecords(t *testing.T) {
	c := &collector{}
	srv := httptest.NewServer(c.handler())
	defer srv.Close()

	shipper := newLogShipper(config.LogShip{Enabled: true, Endpoint: srv.URL, Timeout: 2 * time.Second})
	logger, _ := loggerWithShipper(shipper)
	logger.Info("last words")
	shipper.Close() // immediately, well inside flushInterval

	if got := c.got(); len(got) != 1 || got[0]["msg"] != "last words" {
		t.Errorf("Close must flush what is queued, got %v", got)
	}
}
