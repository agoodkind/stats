package logging

import (
	"context"
	"io"
	"log/slog"

	"goodkind.io/gklog"
)

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

func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return gklog.WithLogger(ctx, logger)
}

func LoggerFromContext(ctx context.Context) *slog.Logger {
	return gklog.LoggerFromContext(ctx)
}
