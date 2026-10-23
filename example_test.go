package slogx_test

import (
	"context"
	"log/slog"
	"os"

	"github.com/amaterasutears/slogx"
)

func ExampleNewContextHandler() {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		// Omit the timestamp so the example's output is reproducible.
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	})
	logger := slog.New(slogx.NewContextHandler(handler))
	ctx := slogx.WithAttrs(context.Background(), slog.String("request_id", "req-123"))
	ctx = slogx.WithAttrs(ctx, slog.Int64("user_id", 42))
	logger.With("component", "person_service").WithGroup("database").InfoContext(
		ctx, "user loaded", slog.String("source", "postgres"),
	)
	// Output:
	// {"level":"INFO","msg":"user loaded","component":"person_service","database":{"source":"postgres","request_id":"req-123","user_id":42}}
}
