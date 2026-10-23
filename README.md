# slogx

`slogx` adds context attributes to the standard `log/slog` through a handler
decorator. Use the standard `*slog.Logger`; no additional logger API is needed.
Requires Go 1.21 or later and has no dependencies outside the standard library.

```go
package main

import (
    "context"
    "log/slog"
    "os"

    "github.com/amaterasutears/slogx"
)

func main() {
    handler := slogx.NewContextHandler(slog.NewJSONHandler(os.Stdout, nil))
    logger := slog.New(handler)

    ctx := slogx.WithAttrs(context.Background(), slog.String("request_id", "req-123"))
    ctx = slogx.WithAttrs(ctx, slog.Int64("user_id", 42))

    logger.InfoContext(ctx, "user loaded", slog.String("source", "postgres"))
}
```

The output includes the standard `time`, `level`, and `msg` fields, followed by
`source`, `request_id`, and `user_id`. An executable example, including groups
and persistent attributes, is in [example_test.go](example_test.go).

## Attribute semantics

- Each `WithAttrs(ctx, attrs...)` call copies the parent's attributes followed by
  the new attributes, preserving their order. Parent and sibling contexts are
  unaffected. Changing the input slice afterward does not change the context.
  An empty attribute list returns the original context without allocating.
- `logger.With(...)` delegates persistent attributes to the wrapped handler.
  Derived loggers preserve context support and leave the original logger intact.
- Context attributes are appended after the record's attributes at log time.
  They belong to the logger's current group. For example,
  `logger.With("component", "service").WithGroup("database").InfoContext(ctx, "loaded")`
  keeps `component` at the root and puts `request_id` and `user_id` inside
  `database`. Nested `WithGroup` calls nest the context attributes further.
  Persistent attributes retain the group active when they were added.
  An empty group name has no effect.
- Keys are not deduplicated. All attributes are passed to the wrapped handler.
  With the standard JSON handler, persistent attributes come first, then record
  attributes, then context attributes in insertion order. Matching keys in the
  same group produce duplicate JSON members. Consumers decide how to interpret
  these; `encoding/json` decoding into a map retains the last value. `slogx` does
  not implement an overriding or merging policy.
- The wrapped handler controls formatting, level filtering, `LogValuer`
  resolution, empty attributes, and attribute groups. Cancellation does not
  suppress logging. Errors from its `Handle` method are returned unchanged;
  standard `slog.Logger` output methods discard handler errors.

## Concurrency and limits

Attribute storage is immutable after construction. A context can be shared
between goroutines, including while creating independent child contexts.
`ContextHandler` stores only its wrapped handler, never a request context.
Its `WithAttrs` and `WithGroup` methods return the receiver for empty inputs;
otherwise they create new decorators. All methods can be called concurrently
if the wrapped handler meets the standard `slog.Handler`
concurrency contract, as the standard JSON and text handlers do.

Before adding context attributes, the handler calls `Record.Clone()` to avoid
modifying a shared record's backing storage. When no context attributes are
present, it forwards the record without allocating attribute storage.

Attribute values, including arbitrary objects, slices, and group contents, are
not deep-copied. Callers must keep them immutable or synchronize access as needed.
Attribute accumulation copies the complete attribute list for each nonempty
`WithAttrs` call. Prefer adding a batch when appropriate for your application.
Pass a non-nil context to `WithAttrs` and a non-nil handler to
`NewContextHandler`; the zero value of `ContextHandler` is not usable.

## Verification

```sh
go test ./...
go test -race ./...
go vet ./...
go test -run '^$' -bench . -benchmem ./...
```

Tests cover context isolation, ownership of input slices, record cloning,
groups, duplicate keys, filtering, error propagation, concurrent use, and
`testing/slogtest` conformance. Benchmarks compare the standard JSON handler
with the decorator with and without context attributes. Setup runs outside the
timed loop, and output goes to `io.Discard`.

The implementation follows the official [slog.Handler contract](https://pkg.go.dev/log/slog#Handler),
[handler guide](https://go.dev/s/slog-handler-guide), and
[testing/slogtest documentation](https://pkg.go.dev/testing/slogtest).
