package utils

import (
	"log/slog"
	"os"
)

// Logger is the global logger instance used across the edge codebase.
var Logger *slog.Logger

func init() {
	Logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
}
