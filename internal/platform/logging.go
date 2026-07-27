package platform

import (
	"log/slog"
	"os"

	"github.com/omni/bugtracker/internal/config"
)

// NewLogger builds a structured slog logger writing JSON to stdout.
//
// When log shipping is configured it also fans every record into a batched POST to
// Omni-Logging; the returned shipper must be Closed at shutdown to flush. A nil
// shipper (shipping disabled) is safe to Close.
func NewLogger(cfg config.Log) (*slog.Logger, *LogShipper) {
	level := slog.LevelInfo
	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if cfg.Format == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	// Shipping is additive: stdout keeps working exactly as before, which is what
	// you need when the log server is the thing that has broken.
	shipper := newLogShipper(cfg.Ship)
	if shipper != nil {
		handler = fanoutHandler{handlers: []slog.Handler{
			handler,
			// Always JSON for the wire, whatever stdout is set to — Omni-Logging
			// parses structured events, and text format would arrive as opaque raw.
			slog.NewJSONHandler(shipper, opts),
		}}
	}

	logger := slog.New(handler).With("service", "omni-bugtracker")
	slog.SetDefault(logger)
	return logger, shipper
}
