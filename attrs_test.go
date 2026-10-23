package slogx_test

import (
	"bytes"
	"context"
	"log/slog"
	"reflect"
	"testing"

	"github.com/amaterasutears/slogx"
)

func TestWithAttrs(t *testing.T) {
	tests := []struct {
		name    string
		batches [][]slog.Attr
		want    []slog.Attr
	}{
		{name: "none"},
		{
			name:    "one",
			batches: [][]slog.Attr{{slog.String("request_id", "req-123")}},
			want:    []slog.Attr{slog.String("request_id", "req-123")},
		},
		{
			name: "several",
			batches: [][]slog.Attr{
				{slog.String("request_id", "req-123"), slog.Int64("user_id", 42)},
			},
			want: []slog.Attr{slog.String("request_id", "req-123"), slog.Int64("user_id", 42)},
		},
		{
			name: "successive calls preserve order and duplicate keys",
			batches: [][]slog.Attr{
				{slog.Int("id", 1)},
				nil,
				{slog.Int("id", 2), slog.Int("id", 3)},
			},
			want: []slog.Attr{slog.Int("id", 1), slog.Int("id", 2), slog.Int("id", 3)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			for _, attrs := range tt.batches {
				ctx = slogx.WithAttrs(ctx, attrs...)
			}
			h := slogx.NewContextHandler(
				&probeHandler{handle: func(gotCtx context.Context, r slog.Record) error {
					if gotCtx != ctx {
						t.Error("Handle changed the context")
					}
					assertAttrs(t, recordAttrs(r), tt.want)
					return nil
				}},
			)
			if err := h.Handle(ctx, slog.Record{}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestWithAttrsEmptyReturnsSameContext(t *testing.T) {
	for _, ctx := range []context.Context{
		context.Background(),
		slogx.WithAttrs(context.Background(), slog.Int("id", 1)),
	} {
		if got := slogx.WithAttrs(ctx); got != ctx {
			t.Error("empty WithAttrs created a new context")
		}
		var attrs []slog.Attr
		if got := slogx.WithAttrs(ctx, attrs...); got != ctx {
			t.Error("nil attributes created a new context")
		}
	}
}

func TestWithAttrsContextIsolation(t *testing.T) {
	// Several generations exercise implementations that accidentally reuse spare
	// capacity when appending to a parent's attribute slice.
	root := context.Background()
	parent := root
	for i := 0; i < 7; i++ {
		parent = slogx.WithAttrs(parent, slog.Int("parent", i))
	}
	left := slogx.WithAttrs(parent, slog.String("child", "left"))
	right := slogx.WithAttrs(parent, slog.String("child", "right"))
	grandchild := slogx.WithAttrs(left, slog.Bool("grandchild", true))
	var buf bytes.Buffer
	logger := slog.New(slogx.NewContextHandler(slog.NewJSONHandler(&buf, nil)))
	for _, ctx := range []context.Context{root, parent, left, right, grandchild, parent, left} {
		logger.InfoContext(ctx, "isolation")
	}
	got := parseEntries(t, buf.Bytes())
	if len(got) != 7 {
		t.Fatalf("got %d entries, want 7", len(got))
	}
	for i, want := range []map[string]any{
		{},
		{"parent": float64(6)},
		{"parent": float64(6), "child": "left"},
		{"parent": float64(6), "child": "right"},
		{"parent": float64(6), "child": "left", "grandchild": true},
		{"parent": float64(6)},
		{"parent": float64(6), "child": "left"},
	} {
		assertFields(t, got[i], "INFO", "isolation", want)
	}
}

func TestWithAttrsCopiesInput(t *testing.T) {
	attrs := make([]slog.Attr, 1, 8)
	attrs[0] = slog.String("request_id", "original")
	parent := slogx.WithAttrs(context.Background(), attrs...)
	attrs[0] = slog.String("request_id", "changed")
	child := slogx.WithAttrs(parent, attrs...)
	attrs[0] = slog.String("request_id", "changed again")
	attrs = append(attrs, slog.Bool("unexpected", true))
	for _, tt := range []struct {
		ctx  context.Context
		want []slog.Attr
	}{
		{parent, []slog.Attr{slog.String("request_id", "original")}},
		{child, []slog.Attr{slog.String("request_id", "original"), slog.String("request_id", "changed")}},
	} {
		h := slogx.NewContextHandler(
			&probeHandler{handle: func(_ context.Context, r slog.Record) error {
				assertAttrs(t, recordAttrs(r), tt.want)
				return nil
			}},
		)
		if err := h.Handle(tt.ctx, slog.Record{}); err != nil {
			t.Fatal(err)
		}
	}
}

func recordAttrs(r slog.Record) []slog.Attr {
	var attrs []slog.Attr
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})
	return attrs
}

func assertAttrs(t *testing.T, got, want []slog.Attr) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("attributes = %v, want %v", got, want)
	}
}
