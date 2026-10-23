// Package slogx extends log/slog with context attributes while keeping the
// standard slog.Logger API.
package slogx

import (
	"context"
	"log/slog"
)

type attrsKey struct{}

// WithAttrs returns a context containing the parent's logging attributes
// followed by attrs. It copies the attribute slice, leaving the parent and
// sibling contexts unchanged. Values inside attributes are not deep-copied.
// If attrs is empty, WithAttrs returns ctx unchanged.
// As with context.WithValue, ctx must be non-nil.
func WithAttrs(ctx context.Context, attrs ...slog.Attr) context.Context {
	if len(attrs) == 0 {
		return ctx
	}
	a := attrsFromContext(ctx)
	return context.WithValue(ctx, attrsKey{}, appendAttrs(a, attrs...))
}

func attrsFromContext(ctx context.Context) []slog.Attr {
	a, _ := ctx.Value(attrsKey{}).([]slog.Attr)
	return a
}

func appendAttrs(a []slog.Attr, attrs ...slog.Attr) []slog.Attr {
	at := make([]slog.Attr, 0, len(a)+len(attrs))
	at = append(at, a...)
	at = append(at, attrs...)
	return at
}
