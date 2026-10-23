package slogx

import (
	"context"
	"log/slog"
)

// ContextHandler decorates a slog.Handler with attributes stored by WithAttrs.
// Context attributes follow the record's attributes and belong to the current
// group established by WithGroup. Duplicate keys are passed through unchanged.
//
// ContextHandler has no mutable state. Its methods may be called concurrently
// provided the wrapped handler satisfies the slog.Handler concurrency contract.
// The zero value is not usable; construct a handler with NewContextHandler.
type ContextHandler struct {
	h slog.Handler
}

// NewContextHandler returns a handler that adds context attributes before
// delegating to handler. The supplied handler must be non-nil.
func NewContextHandler(h slog.Handler) *ContextHandler {
	return &ContextHandler{
		h: h,
	}
}

// Enabled delegates the level check to the wrapped handler.
func (h *ContextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.h.Enabled(ctx, level)
}

// Handle appends context attributes to a clone of r, if any are present, and
// returns the wrapped handler's error. Without context attributes it forwards r
// unchanged, without allocating additional attribute storage.
func (h *ContextHandler) Handle(ctx context.Context, r slog.Record) error {
	if attrs, ok := ctx.Value(attrsKey{}).([]slog.Attr); ok && len(attrs) > 0 {
		r = r.Clone()
		r.AddAttrs(attrs...)
	}
	return h.h.Handle(ctx, r)
}

// WithAttrs returns a new ContextHandler wrapping the handler's WithAttrs
// result. The wrapped handler takes ownership of attrs, as in slog.Handler.
// If attrs is empty, WithAttrs returns h unchanged.
func (h *ContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	return NewContextHandler(h.h.WithAttrs(attrs))
}

// WithGroup returns a new ContextHandler wrapping the handler's WithGroup
// result. Context attributes are added within the resulting group, including
// any outer groups. An empty name returns h unchanged.
func (h *ContextHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return NewContextHandler(h.h.WithGroup(name))
}
