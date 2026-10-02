// Package logging carries the CLI's logger through application request contexts.
package logging

import (
	"context"
	"io"
	"log/slog"
)

type contextKey struct{}

func WithContext(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, logger)
}

func FromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(contextKey{}).(*slog.Logger); ok {
		return logger
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
