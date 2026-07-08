package platform

import (
	"log/slog"
	"os"

	"github.com/omni/bugtracker/internal/config"
)

// NewLogger builds a structured slog logger. JSON output ships cleanly to Omni-Logging.
func NewLogger(cfg config.Log) *slog.Logger {
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
	logger := slog.New(handler).With("service", "omni-bugtracker")
	slog.SetDefault(logger)
	return logger
}
