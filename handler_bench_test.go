package slogx_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/amaterasutears/slogx"
)

func BenchmarkJSONHandler(b *testing.B) {
	benchmarkLogger(b, slog.NewJSONHandler(io.Discard, nil), context.Background())
}

func BenchmarkContextHandlerNoAttrs(b *testing.B) {
	benchmarkLogger(b, slogx.NewContextHandler(slog.NewJSONHandler(io.Discard, nil)), context.Background())
}

func BenchmarkContextHandlerWithAttrs(b *testing.B) {
	ctx := slogx.WithAttrs(context.Background(),
		slog.String("request_id", "req-123"),
		slog.Int64("user_id", 42),
		slog.String("tenant", "example"),
	)
	benchmarkLogger(b, slogx.NewContextHandler(slog.NewJSONHandler(io.Discard, nil)), ctx)
}

func benchmarkLogger(b *testing.B, handler slog.Handler, ctx context.Context) {
	b.Helper()
	logger := slog.New(handler)
	attr := slog.String("source", "postgres")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.LogAttrs(ctx, slog.LevelInfo, "user loaded", attr)
	}
}
