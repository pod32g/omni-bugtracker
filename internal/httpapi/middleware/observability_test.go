package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// capture runs one request through RequestLogger and returns whatever it logged.
// Level is Debug so the "routine request" case is visible to the test even though
// production ships at info and will not carry it.
func capture(t *testing.T, path string, h http.HandlerFunc) []map[string]any {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	rr := httptest.NewRecorder()
	RequestLogger(logger)(h).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))

	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		out = append(out, rec)
	}
	return out
}

func okHandler(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }

// The endpoints machines poll must produce nothing at all. Prometheus scraping /metrics
// every 15s was on its own half of everything we shipped to Omni-Logging.
func TestRequestLoggerSkipsOpsEndpoints(t *testing.T) {
	for _, path := range []string{"/metrics", "/healthz", "/readyz"} {
		if got := capture(t, path, okHandler); len(got) != 0 {
			t.Errorf("%s logged %d lines, want 0: %v", path, len(got), got)
		}
	}
}

// A request that worked is a measurement, not an event — the Prometheus counters answer
// "how many". Keeping it at debug means it is there when you are debugging and absent
// from the shipped stream the rest of the time.
func TestRequestLoggerRoutineRequestIsDebug(t *testing.T) {
	got := capture(t, "/api/v1/issues", okHandler)
	if len(got) != 1 {
		t.Fatalf("got %d lines, want 1", len(got))
	}
	if got[0]["level"] != "DEBUG" {
		t.Errorf("level = %v, want DEBUG", got[0]["level"])
	}
	if got[0]["msg"] != "http_request" {
		t.Errorf("msg = %v, want http_request", got[0]["msg"])
	}
}

// A 5xx is the thing you actually go to the log for.
func TestRequestLoggerFailureIsError(t *testing.T) {
	got := capture(t, "/api/v1/issues", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	if len(got) != 1 {
		t.Fatalf("got %d lines, want 1", len(got))
	}
	if got[0]["level"] != "ERROR" {
		t.Errorf("level = %v, want ERROR", got[0]["level"])
	}
	if got[0]["msg"] != "http_request_failed" {
		t.Errorf("msg = %v, want http_request_failed", got[0]["msg"])
	}
	if got[0]["status"] != float64(500) {
		t.Errorf("status = %v, want 500", got[0]["status"])
	}
}

// A 401 is an ordinary answer, not a failure: an expired tab produced thousands of them
// and every one was noise. It must not escalate the way a 5xx does.
func TestRequestLoggerUnauthorizedIsNotAnError(t *testing.T) {
	got := capture(t, "/api/v1/me/notifications", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	if len(got) != 1 {
		t.Fatalf("got %d lines, want 1", len(got))
	}
	if got[0]["level"] != "DEBUG" {
		t.Errorf("level = %v, want DEBUG", got[0]["level"])
	}
}

// A request that succeeds but takes far too long is a symptom, and the status code
// cannot express it — 200 in 30s and 200 in 3ms are the same line otherwise.
func TestRequestLoggerSlowRequestIsWarn(t *testing.T) {
	restore := slowRequest
	slowRequest = time.Millisecond
	t.Cleanup(func() { slowRequest = restore })

	got := capture(t, "/api/v1/reports", func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(5 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})
	if len(got) != 1 {
		t.Fatalf("got %d lines, want 1", len(got))
	}
	if got[0]["level"] != "WARN" {
		t.Errorf("level = %v, want WARN", got[0]["level"])
	}
	if got[0]["msg"] != "http_request_slow" {
		t.Errorf("msg = %v, want http_request_slow", got[0]["msg"])
	}
}

// Being slow must not outrank being broken: a 500 that also ran long is still a failure.
func TestRequestLoggerFailureOutranksSlow(t *testing.T) {
	restore := slowRequest
	slowRequest = time.Millisecond
	t.Cleanup(func() { slowRequest = restore })

	got := capture(t, "/api/v1/reports", func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(5 * time.Millisecond)
		w.WriteHeader(http.StatusInternalServerError)
	})
	if got[0]["msg"] != "http_request_failed" {
		t.Errorf("msg = %v, want http_request_failed", got[0]["msg"])
	}
}

// The default must stay clear of ordinary API work, or the warn path becomes the noise
// it was meant to replace.
func TestSlowRequestThresholdIsNotTwitchy(t *testing.T) {
	if slowRequest < time.Second {
		t.Errorf("slowRequest = %v; a threshold this low would fire on ordinary work", slowRequest)
	}
}

// slog.Duration marshals to nanoseconds ("elapsed":1458746), which reads as a garbage
// integer in a log viewer. The field must be milliseconds and must say so in its name.
func TestRequestLoggerReportsMilliseconds(t *testing.T) {
	got := capture(t, "/api/v1/issues", okHandler)
	if _, isDuration := got[0]["elapsed"]; isDuration {
		t.Error("raw nanosecond 'elapsed' is back")
	}
	ms, present := got[0]["elapsed_ms"]
	if !present {
		t.Fatal("elapsed_ms missing")
	}
	if ms.(float64) > 1000 {
		t.Errorf("elapsed_ms = %v; that is not milliseconds", ms)
	}
}
