package obs

import (
	"context"
	"crypto/rand"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const (
	HeaderRequestID = "X-Request-ID"
	HeaderTraceID   = "X-Trace-ID"
)

type requestInfo struct {
	id string

	mu      sync.Mutex
	actor   string
	actorID string
	route   string
}

func (i *requestInfo) actorValues() (string, string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.actor, i.actorID
}

func requestInfoFrom(ctx context.Context) *requestInfo {
	i, _ := ctx.Value(ctxRequest).(*requestInfo)
	return i
}

func RequestID(ctx context.Context) string {
	if i := requestInfoFrom(ctx); i != nil {
		return i.id
	}
	return ""
}

func SetActor(ctx context.Context, username, userID string) {
	if i := requestInfoFrom(ctx); i != nil {
		i.mu.Lock()
		i.actor, i.actorID = username, userID
		i.mu.Unlock()
	}
	trace.SpanFromContext(ctx).SetAttributes(attribute.String("enduser.id", userID))
}

// SetRoute nyatat pola route biar nama span nggak pakai path mentah.
func SetRoute(ctx context.Context, pattern string) {
	if i := requestInfoFrom(ctx); i != nil {
		i.mu.Lock()
		i.route = pattern
		i.mu.Unlock()
	}
	if pattern != "" {
		span := trace.SpanFromContext(ctx)
		span.SetName(pattern)
		span.SetAttributes(attribute.String("http.route", pattern))
	}
}

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// Middleware buka span dan nulis satu log "http.request" per request.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		reqID := r.Header.Get(HeaderRequestID)
		if !validRequestID.MatchString(reqID) {
			reqID = strings.ToLower(rand.Text())
		}
		info := &requestInfo{id: reqID}

		ctx := propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx, span := tracer().Start(ctx, r.Method, trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				attribute.String("http.request.method", r.Method),
				attribute.String("url.path", r.URL.Path),
				attribute.String("http.request_id", reqID),
			))
		defer span.End()
		ctx = context.WithValue(ctx, ctxRequest, info)

		w.Header().Set(HeaderRequestID, reqID)
		if sc := span.SpanContext(); sc.IsValid() {
			w.Header().Set(HeaderTraceID, sc.TraceID().String())
		}
		sw := &statusWriter{ResponseWriter: w}

		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				Event(ctx, slog.LevelError, "http.panic", "panic di handler", "panic", rec)
				span.SetStatus(codes.Error, "panic")
				if !sw.wrote {
					http.Error(sw, "terjadi kesalahan di server", http.StatusInternalServerError)
				}
			}

			status := sw.status
			if status == 0 {
				status = http.StatusOK
			}
			span.SetAttributes(attribute.Int("http.response.status_code", status))
			if status >= 500 {
				span.SetStatus(codes.Error, http.StatusText(status))
			}

			info.mu.Lock()
			route := info.route
			info.mu.Unlock()

			level := slog.LevelInfo
			switch {
			case status >= 500:
				level = slog.LevelError
			case status >= 400:
				level = slog.LevelWarn
			case strings.HasPrefix(r.URL.Path, "/assets/"):
				level = slog.LevelDebug
			}
			Event(ctx, level, "http.request", "request selesai",
				"method", r.Method,
				"path", r.URL.Path,
				"route", route,
				KeyStatus, status,
				"duration_ms", float64(time.Since(start).Microseconds())/1000,
				"bytes", sw.bytes,
			)
		}()

		next.ServeHTTP(sw, r.WithContext(ctx))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
	wrote  bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wrote {
		w.status = code
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
