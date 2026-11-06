package obs

import (
	"context"
	"io"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

const (
	KeyEvent       = "event"
	KeyActor       = "actor"
	KeyActorID     = "actor_id"
	KeyDatabase    = "database"
	KeyStatus      = "status"
	KeyRequestID   = "request_id"
	KeyTraceID     = "trace_id"
	KeySpanID      = "span_id"
	KeyMigrationID = "migration_id"
	KeyJobID       = "job_id"
	KeyExecutionID = "execution_id"
	KeyAttempt     = "attempt"
	KeyBatch       = "batch"
)

type ctxKey int

const (
	ctxAttrs ctxKey = iota
	ctxRequest
	ctxQuery
)

// NewLogger bikin logger JSON yang otomatis nambahin ID korelasi dari context.
func NewLogger(w io.Writer, level slog.Leveler) *slog.Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level, ReplaceAttr: redactAttr})
	return slog.New(&ctxHandler{inner: h})
}

type ctxHandler struct {
	inner slog.Handler
}

func (h *ctxHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h *ctxHandler) Handle(ctx context.Context, r slog.Record) error {
	if ctx != nil {
		if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
			r.AddAttrs(slog.String(KeyTraceID, sc.TraceID().String()), slog.String(KeySpanID, sc.SpanID().String()))
		}
		if info := requestInfoFrom(ctx); info != nil {
			r.AddAttrs(slog.String(KeyRequestID, info.id))
			if actor, actorID := info.actorValues(); actor != "" && !hasAttr(r, KeyActor) {
				r.AddAttrs(slog.String(KeyActor, actor), slog.String(KeyActorID, actorID))
			}
		}
		if extra, ok := ctx.Value(ctxAttrs).([]slog.Attr); ok {
			r.AddAttrs(extra...)
		}
	}
	return h.inner.Handle(ctx, r)
}

func (h *ctxHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ctxHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *ctxHandler) WithGroup(name string) slog.Handler {
	return &ctxHandler{inner: h.inner.WithGroup(name)}
}

func hasAttr(r slog.Record, key string) bool {
	found := false
	r.Attrs(func(a slog.Attr) bool {
		found = a.Key == key
		return !found
	})
	return found
}

// WithAttrs nempelin atribut ke context supaya ikut ke semua log turunannya.
func WithAttrs(ctx context.Context, args ...any) context.Context {
	var r slog.Record
	r.Add(args...)
	prev, _ := ctx.Value(ctxAttrs).([]slog.Attr)
	next := append([]slog.Attr(nil), prev...)
	r.Attrs(func(a slog.Attr) bool {
		next = append(next, a)
		return true
	})
	return context.WithValue(ctx, ctxAttrs, next)
}

func Event(ctx context.Context, level slog.Level, event, msg string, args ...any) {
	slog.Default().Log(ctx, level, msg, append([]any{KeyEvent, event}, args...)...)
}

func Debug(ctx context.Context, event, msg string, args ...any) {
	Event(ctx, slog.LevelDebug, event, msg, args...)
}

func Info(ctx context.Context, event, msg string, args ...any) {
	Event(ctx, slog.LevelInfo, event, msg, args...)
}

func Warn(ctx context.Context, event, msg string, args ...any) {
	Event(ctx, slog.LevelWarn, event, msg, args...)
}

func Error(ctx context.Context, event, msg string, err error, args ...any) {
	if err != nil {
		args = append(args, "error", err.Error())
	}
	Event(ctx, slog.LevelError, event, msg, args...)
}