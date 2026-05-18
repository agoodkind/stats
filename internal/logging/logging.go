// Package logging is the stats-gh slog setup: it builds the structured
// logger, sets it as the process default, and exposes context-carrying
// helpers.
package logging

import (
	"context"
	"io"
	"log/slog"

	"goodkind.io/gklog"
)

// New builds the [slog.Logger] used throughout the binary, returning the
// logger and its closer (call Close before the process exits).
func New(level string, buildVersion string) (*slog.Logger, io.Closer) {
	logger, closer := gklog.New(gklog.Config{
		BuildVersion: buildVersion,
		Handlers: []slog.Handler{
			gklog.StdoutJSON(gklog.ParseLevel(level)),
		},
	})
	slog.SetDefault(logger)
	return logger, closer
}

// WithLogger returns a context carrying the supplied logger so downstream
// callers can retrieve it via LoggerFromContext.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return gklog.WithLogger(ctx, logger)
}

// LoggerFromContext returns the logger previously attached with WithLogger,
// or the process default if none is attached.
func LoggerFromContext(ctx context.Context) *slog.Logger {
	return gklog.LoggerFromContext(ctx)
}
