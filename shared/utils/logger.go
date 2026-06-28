package utils

import (
	"context"
	"log/slog"
	"os"
)

type LoggerKey struct{}

// Logger is the global logger instance used across the edge codebase.
var Logger *slog.Logger

func init() {
	Logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
}

func ContextWithLogger(ctx context.Context, logger *slog.Logger, reqID *string) context.Context {
	if reqID != nil {
		logger = logger.With("request_id", *reqID)
	}
	return context.WithValue(ctx, LoggerKey{}, logger)
}

func LoggerFromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(LoggerKey{}).(*slog.Logger); ok {
		return logger
	}
	return Logger
}
