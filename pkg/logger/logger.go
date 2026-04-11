package logger

import (
	"context"
	"log/slog"
	"os"
)

type ctxKey struct{}

func New(env string) *slog.Logger {
	var handler slog.Handler
	// In production we elide the per-record time formatting cost by emitting
	// a Unix-millisecond int instead of RFC3339 text, and we skip AddSource
	// entirely (runtime.Caller() on every log line is surprisingly expensive
	// under load). Dev keeps the richer format for easier debugging.
	opts := &slog.HandlerOptions{
		AddSource: env != "production",
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if env == "production" && a.Key == slog.TimeKey {
				t := a.Value.Time()
				return slog.Int64("ts_ms", t.UnixMilli())
			}
			return a
		},
	}

	if env == "production" {
		opts.Level = slog.LevelInfo
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		opts.Level = slog.LevelDebug
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}

func WithContext(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, logger)
}

func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}
