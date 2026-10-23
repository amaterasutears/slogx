package slogx_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
	"testing"
	"testing/slogtest"
	"time"

	"github.com/amaterasutears/slogx"
)

// probeHandler overrides only the operations observed by a test. Other methods
// deliberately fail through the nil embedded handler if called unexpectedly.
type probeHandler struct {
	slog.Handler
	enabled func(context.Context, slog.Level) bool
	handle  func(context.Context, slog.Record) error
}

func (h *probeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.enabled(ctx, level)
}

func (h *probeHandler) Handle(ctx context.Context, r slog.Record) error {
	return h.handle(ctx, r)
}

func TestContextHandlerJSON(t *testing.T) {
	tests := []struct {
		name   string
		derive func(*slog.Logger) *slog.Logger
		want   map[string]any
	}{
		{"base", func(l *slog.Logger) *slog.Logger { return l }, map[string]any{
			"request_id": "req-123", "user_id": float64(42), "source": "postgres",
		}},
		{"With", func(l *slog.Logger) *slog.Logger { return l.With("component", "person_service") }, map[string]any{
			"component": "person_service", "request_id": "req-123", "user_id": float64(42), "source": "postgres",
		}},
		{"WithGroup", func(l *slog.Logger) *slog.Logger { return l.WithGroup("database") }, map[string]any{
			"database": map[string]any{"request_id": "req-123", "user_id": float64(42), "source": "postgres"},
		}},
		{"nested groups and persistent attributes", func(l *slog.Logger) *slog.Logger {
			return l.With("component", "person_service").WithGroup("database").With("driver", "pgx").WithGroup("query").With("operation", "select")
		}, map[string]any{
			"component": "person_service",
			"database": map[string]any{
				"driver": "pgx",
				"query": map[string]any{
					"operation": "select", "request_id": "req-123", "user_id": float64(42), "source": "postgres",
				},
			},
		}},
		{"empty group", func(l *slog.Logger) *slog.Logger { return l.WithGroup("") }, map[string]any{
			"request_id": "req-123", "user_id": float64(42), "source": "postgres",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(slogx.NewContextHandler(slog.NewJSONHandler(&buf, nil)))
			ctx := slogx.WithAttrs(context.Background(), slog.String("request_id", "req-123"))
			ctx = slogx.WithAttrs(ctx, slog.Int64("user_id", 42))
			tt.derive(logger).InfoContext(ctx, "user loaded", slog.String("source", "postgres"))
			entries := parseEntries(t, buf.Bytes())
			if len(entries) != 1 {
				t.Fatalf("got %d entries, want 1", len(entries))
			}
			assertFields(t, entries[0], "INFO", "user loaded", tt.want)
		})
	}
}

func TestContextHandlerEmptyDerivation(t *testing.T) {
	tests := []struct {
		name   string
		derive func(*slogx.ContextHandler) slog.Handler
	}{
		{"nil attributes", func(h *slogx.ContextHandler) slog.Handler { return h.WithAttrs(nil) }},
		{"empty attributes", func(h *slogx.ContextHandler) slog.Handler { return h.WithAttrs([]slog.Attr{}) }},
		{"empty group", func(h *slogx.ContextHandler) slog.Handler { return h.WithGroup("") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			inner := slog.NewJSONHandler(&buf, nil).
				WithAttrs([]slog.Attr{slog.String("component", "service")}).
				WithGroup("database")
			h := slogx.NewContextHandler(inner)
			got := tt.derive(h)
			if got != h {
				t.Fatal("empty derivation must return its receiver")
			}
			ctx := slogx.WithAttrs(context.Background(), slog.String("request_id", "req-123"))
			slog.New(got).InfoContext(ctx, "unchanged")
			entries := parseEntries(t, buf.Bytes())
			if len(entries) != 1 {
				t.Fatalf("got %d entries, want 1", len(entries))
			}
			assertFields(t, entries[0], "INFO", "unchanged", map[string]any{
				"component": "service", "database": map[string]any{"request_id": "req-123"},
			})
		})
	}
}

func TestContextHandlerDerivedLoggers(t *testing.T) {
	var buf bytes.Buffer
	h := slogx.NewContextHandler(slog.NewJSONHandler(&buf, nil))
	base := slog.New(h)
	first := base.With("component", "service")
	grouped := first.WithGroup("database")
	second := grouped.With("driver", "pgx")
	sibling := first.With("sibling", true)
	ctx := slogx.WithAttrs(context.Background(), slog.Int("id", 42))
	for _, logger := range []*slog.Logger{base, first, grouped, second, sibling, base} {
		logger.InfoContext(ctx, "derived")
	}
	entries := parseEntries(t, buf.Bytes())
	wants := []map[string]any{
		{"id": float64(42)},
		{"component": "service", "id": float64(42)},
		{"component": "service", "database": map[string]any{"id": float64(42)}},
		{"component": "service", "database": map[string]any{"driver": "pgx", "id": float64(42)}},
		{"component": "service", "sibling": true, "id": float64(42)},
		{"id": float64(42)},
	}
	if len(entries) != len(wants) {
		t.Fatalf("got %d entries, want %d", len(entries), len(wants))
	}
	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte{'\n'})
	for i, want := range wants {
		assertFields(t, entries[i], "INFO", "derived", want)
		// Maps hide duplicate keys, so also inspect JSON tokens.
		keys := jsonKeys(t, lines[i])
		if got, want := len(keys), 3+fieldCount(want); got != want {
			t.Errorf("entry %d has %d keys, want %d: %v", i, got, want, keys)
		}
	}
}

func TestContextHandlerDuplicateKeys(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slogx.NewContextHandler(slog.NewJSONHandler(&buf, nil))).With("id", "persistent")
	ctx := slogx.WithAttrs(context.Background(), slog.String("id", "parent"))
	ctx = slogx.WithAttrs(ctx, slog.String("id", "child"))
	logger.InfoContext(ctx, "duplicates", slog.String("id", "record"))
	keys := jsonKeys(t, buf.Bytes())
	if want := []string{"time", "level", "msg", "id", "id", "id", "id"}; !reflect.DeepEqual(keys, want) {
		t.Errorf("keys = %v, want %v", keys, want)
	}
	dec := json.NewDecoder(&buf)
	var values []string
	for {
		token, err := dec.Token()
		if err != nil {
			break
		}
		if token == "id" {
			value, err := dec.Token()
			if err != nil {
				t.Fatal(err)
			}
			values = append(values, value.(string))
		}
	}
	if want := []string{"persistent", "record", "parent", "child"}; !reflect.DeepEqual(values, want) {
		t.Errorf("duplicate values = %v, want %v", values, want)
	}
}

type noValueContext struct{ context.Context }

func (noValueContext) Value(any) any { panic("Enabled must not inspect context attributes") }

func TestContextHandlerEnabledDelegates(t *testing.T) {
	ctx := noValueContext{context.Background()}
	calls := 0
	h := slogx.NewContextHandler(&probeHandler{enabled: func(got context.Context, level slog.Level) bool {
		calls++
		if got != ctx {
			t.Error("Enabled changed the context")
		}
		return level >= slog.LevelWarn
	}})
	for _, level := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
		if got, want := h.Enabled(ctx, level), level >= slog.LevelWarn; got != want {
			t.Errorf("Enabled(%v) = %v, want %v", level, got, want)
		}
	}
	// Disabled records must never reach Handle or inspect context values.
	slog.New(h).DebugContext(ctx, "disabled")
	if calls != 5 {
		t.Errorf("Enabled called %d times, want 5", calls)
	}
}

func TestContextHandlerLevels(t *testing.T) {
	var buf bytes.Buffer
	var level slog.LevelVar
	level.Set(slog.LevelWarn)
	logger := slog.New(slogx.NewContextHandler(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: &level})))
	ctx := slogx.WithAttrs(context.Background(), slog.Int("id", 42))
	logger.DebugContext(ctx, "debug")
	logger.InfoContext(ctx, "info")
	logger.WarnContext(ctx, "warn")
	logger.ErrorContext(ctx, "error")
	level.Set(slog.LevelDebug)
	logger.DebugContext(ctx, "debug enabled")
	entries := parseEntries(t, buf.Bytes())
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	for i, want := range []struct{ level, msg string }{{"WARN", "warn"}, {"ERROR", "error"}, {"DEBUG", "debug enabled"}} {
		assertFields(t, entries[i], want.level, want.msg, map[string]any{"id": float64(42)})
	}
}

func TestContextHandlerReturnsError(t *testing.T) {
	want := errors.New("write failed")
	for _, ctx := range []context.Context{context.Background(), slogx.WithAttrs(context.Background(), slog.Int("id", 1))} {
		h := slogx.NewContextHandler(&probeHandler{handle: func(context.Context, slog.Record) error { return want }})
		if got := h.Handle(ctx, slog.Record{}); got != want {
			t.Errorf("Handle error = %v, want %v", got, want)
		}
	}
}

func TestContextHandlerClonesRecord(t *testing.T) {
	r := slog.NewRecord(time.Now(), slog.LevelWarn, "original", 123)
	var original []slog.Attr
	// Incremental appends leave spare capacity beyond the inline attributes.
	for i := 0; i < 8; i++ {
		a := slog.Int(fmt.Sprintf("key%d", i), i)
		r.AddAttrs(a)
		original = append(original, a)
	}
	ctx := slogx.WithAttrs(context.Background(), slog.String("context", "value"))
	want := append(append([]slog.Attr(nil), original...), slog.String("context", "value"))
	h := slogx.NewContextHandler(&probeHandler{handle: func(gotCtx context.Context, got slog.Record) error {
		if gotCtx != ctx || got.Time != r.Time || got.Level != r.Level || got.Message != r.Message || got.PC != r.PC {
			t.Error("Handle changed context or record metadata")
		}
		assertAttrs(t, recordAttrs(got), want)
		return nil
	}})
	for i := 0; i < 2; i++ {
		if err := h.Handle(ctx, r); err != nil {
			t.Fatal(err)
		}
		assertAttrs(t, recordAttrs(r), original)
	}
	// An unsafe append to a shared backing array can leave hidden state even
	// when the original length is unchanged. Appending to the original exposes it.
	a := slog.Bool("after", true)
	r.AddAttrs(a)
	assertAttrs(t, recordAttrs(r), append(original, a))
}

func TestContextHandlerNoAttrsDoesNotAllocate(t *testing.T) {
	h := slogx.NewContextHandler(&probeHandler{handle: func(context.Context, slog.Record) error { return nil }})
	ctx := context.Background()
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "allocation check", 0)
	allocs := testing.AllocsPerRun(100, func() {
		if err := h.Handle(ctx, r); err != nil {
			panic(err)
		}
	})
	if allocs != 0 {
		t.Errorf("Handle without context attributes allocated %v times, want 0", allocs)
	}
}

func TestContextHandlerSlogtest(t *testing.T) {
	var buf bytes.Buffer
	h := slogx.NewContextHandler(slog.NewJSONHandler(&buf, nil))
	if err := slogtest.TestHandler(h, func() []map[string]any { return parseEntries(t, buf.Bytes()) }); err != nil {
		t.Fatal(err)
	}
}

func TestContextHandlerContextValues(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slogx.NewContextHandler(slog.NewJSONHandler(&buf, nil))).WithGroup("scope")
	ctx, cancel := context.WithCancel(slogx.WithAttrs(context.Background(),
		slog.Attr{}, slog.Group("empty"),
		slog.Group("", slog.Int("inlined", 1)),
		slog.Group("nested", slog.Bool("ok", true)),
		slog.Any("lazy", testLogValuer{}),
	))
	cancel()
	logger.InfoContext(ctx, "canceled")
	assertFields(t, parseEntries(t, buf.Bytes())[0], "INFO", "canceled", map[string]any{
		"scope": map[string]any{"inlined": float64(1), "nested": map[string]any{"ok": true}, "lazy": "resolved"},
	})
}

type testLogValuer struct{}

func (testLogValuer) LogValue() slog.Value { return slog.StringValue("resolved") }

func TestContextHandlerConcurrent(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slogx.NewContextHandler(slog.NewJSONHandler(&buf, nil)))
	shared := logger.With("component", "worker").WithGroup("work")
	ctx := slogx.WithAttrs(context.Background(), slog.String("request_id", "shared"))
	const workers, iterations = 12, 20
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			local := shared.With("worker", worker)
			for i := 0; i < iterations; i++ {
				local.InfoContext(slogx.WithAttrs(ctx, slog.Int("iteration", i)), "local")
				shared.InfoContext(ctx, "shared")
			}
		}(worker)
	}
	wg.Wait()
	entries := parseEntries(t, buf.Bytes())
	if len(entries) != workers*iterations*2 {
		t.Fatalf("got %d entries, want %d", len(entries), workers*iterations*2)
	}
	seen := make(map[[2]int]bool)
	sharedCount := 0
	for _, entry := range entries {
		work, ok := entry["work"].(map[string]any)
		if !ok {
			t.Fatalf("missing work group: %v", entry)
		}
		switch entry["msg"] {
		case "local":
			worker, workerOK := work["worker"].(float64)
			iteration, iterationOK := work["iteration"].(float64)
			if !workerOK || !iterationOK || worker < 0 || worker >= workers || iteration < 0 || iteration >= iterations {
				t.Fatalf("invalid local attributes: %v", work)
			}
			key := [2]int{int(worker), int(iteration)}
			if seen[key] {
				t.Errorf("duplicate local entry: %v", key)
			}
			seen[key] = true
			assertFields(t, entry, "INFO", "local", map[string]any{
				"component": "worker", "work": map[string]any{"worker": worker, "iteration": iteration, "request_id": "shared"},
			})
		case "shared":
			sharedCount++
			assertFields(t, entry, "INFO", "shared", map[string]any{
				"component": "worker", "work": map[string]any{"request_id": "shared"},
			})
		default:
			t.Errorf("unexpected entry: %v", entry)
		}
	}
	if len(seen) != workers*iterations || sharedCount != workers*iterations {
		t.Errorf("got %d local and %d shared entries", len(seen), sharedCount)
	}
}

func TestContextHandlerConcurrentRecord(t *testing.T) {
	var buf bytes.Buffer
	h := slogx.NewContextHandler(slog.NewJSONHandler(&buf, nil))
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "shared record", 0)
	for i := 0; i < 8; i++ {
		r.AddAttrs(slog.Int(fmt.Sprintf("key%d", i), i))
	}
	ctx := slogx.WithAttrs(context.Background(), slog.Bool("context", true))
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := h.Handle(ctx, r); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	entries := parseEntries(t, buf.Bytes())
	if len(entries) != 16 {
		t.Fatalf("got %d entries, want 16", len(entries))
	}
	for _, entry := range entries {
		if len(entry) != 12 || entry["context"] != true {
			t.Errorf("unexpected shared record output: %v", entry)
		}
	}
}

func parseEntries(t *testing.T, data []byte) []map[string]any {
	t.Helper()
	var entries []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatalf("decode log entry: %v", err)
		}
		entries = append(entries, entry)
	}
	return entries
}

func assertFields(t *testing.T, got map[string]any, level, msg string, want map[string]any) {
	t.Helper()
	if timestamp, ok := got["time"].(string); !ok {
		t.Error("missing timestamp")
	} else if _, err := time.Parse(time.RFC3339Nano, timestamp); err != nil {
		t.Errorf("invalid timestamp: %v", err)
	}
	fields := make(map[string]any, len(got))
	for key, value := range got {
		if key != "time" {
			fields[key] = value
		}
	}
	wantFields := map[string]any{"level": level, "msg": msg}
	for key, value := range want {
		wantFields[key] = value
	}
	if !reflect.DeepEqual(fields, wantFields) {
		t.Errorf("fields = %#v, want %#v", fields, wantFields)
	}
}

// jsonKeys preserves duplicate keys, which unmarshaling into a map would hide.
func jsonKeys(t *testing.T, data []byte) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	var keys []string
	if token, err := dec.Token(); err != nil || token != json.Delim('{') {
		t.Fatalf("expected object, got %v: %v", token, err)
	}
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, token.(string))
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			t.Fatal(err)
		}
		if len(value) > 0 && value[0] == '{' {
			keys = append(keys, jsonKeys(t, value)...)
		}
	}
	if _, err := dec.Token(); err != nil {
		t.Fatal(err)
	}
	return keys
}

func fieldCount(fields map[string]any) int {
	count := len(fields)
	for _, value := range fields {
		if nested, ok := value.(map[string]any); ok {
			count += fieldCount(nested)
		}
	}
	return count
}
