package obs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func setup(t *testing.T) (*bytes.Buffer, *tracetest.InMemoryExporter) {
	t.Helper()
	prevLogger := slog.Default()
	prevTP := otel.GetTracerProvider()
	t.Cleanup(func() {
		slog.SetDefault(prevLogger)
		otel.SetTracerProvider(prevTP)
	})

	var buf bytes.Buffer
	slog.SetDefault(NewLogger(&buf, slog.LevelDebug))
	exp := tracetest.NewInMemoryExporter()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp)))
	return &buf, exp
}

func logLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("log bukan JSON: %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

func findEvent(lines []map[string]any, event string) map[string]any {
	for _, l := range lines {
		if l[KeyEvent] == event {
			return l
		}
	}
	return nil
}

func TestLogHasTraceAndCustomAttrs(t *testing.T) {
	buf, _ := setup(t)

	ctx, span := Start(context.Background(), "test.op")
	ctx = WithAttrs(ctx, KeyJobID, "job-1", KeyAttempt, 2)
	Info(ctx, "job.started", "job mulai", KeyDatabase, "target")
	End(span, nil)

	line := findEvent(logLines(t, buf), "job.started")
	if line == nil {
		t.Fatal("log job.started nggak ada")
	}
	for _, key := range []string{"time", "level", "msg", KeyTraceID, KeySpanID} {
		if line[key] == nil || line[key] == "" {
			t.Errorf("atribut %q kosong: %v", key, line)
		}
	}
	if line[KeyTraceID] != span.SpanContext().TraceID().String() {
		t.Errorf("trace_id beda dengan span: %v", line[KeyTraceID])
	}
	if line[KeyJobID] != "job-1" || line[KeyAttempt] != float64(2) || line[KeyDatabase] != "target" {
		t.Errorf("atribut dari WithAttrs/argumen hilang: %v", line)
	}
}

func TestRedaction(t *testing.T) {
	buf, _ := setup(t)
	ctx := context.Background()

	Info(ctx, "test.redact", "cek",
		"password", "rahasia1",
		"csrf_token", "rahasia2",
		"sid", "rahasia3",
		"token_hash", "rahasia4",
		"url", "postgres://target:rahasia5@localhost:5436/db",
		"conn", "host=x user=y password=rahasia6 dbname=z",
		"query", "ALTER ROLE app PASSWORD 'rahasia7'",
		"username", "budi",
	)
	out := buf.String()
	for i := 1; i <= 7; i++ {
		secret := "rahasia" + string(rune('0'+i))
		if strings.Contains(out, secret) {
			t.Errorf("%s bocor ke log: %s", secret, out)
		}
	}
	if !strings.Contains(out, "budi") || !strings.Contains(out, "localhost:5436") {
		t.Errorf("nilai yang nggak rahasia jangan ikut hilang: %s", out)
	}
}

func TestInjectExtractTrace(t *testing.T) {
	_, exp := setup(t)

	ctx, publish := StartKind(context.Background(), "queue.publish", trace.SpanKindProducer)
	carrier := InjectTrace(ctx)
	End(publish, nil)
	if carrier["traceparent"] == "" {
		t.Fatalf("traceparent harus ada di carrier: %v", carrier)
	}

	wctx := ExtractTrace(context.Background(), carrier)
	_, work := Start(wctx, "worker.job")
	End(work, errors.New("gagal"))

	spans := exp.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("mau 2 span, dapat %d", len(spans))
	}
	if spans[0].SpanContext.TraceID() != spans[1].SpanContext.TraceID() {
		t.Error("span worker harus satu trace dengan span publish")
	}
	if spans[1].Parent.SpanID() != spans[0].SpanContext.SpanID() {
		t.Error("parent span worker harus span publish")
	}
	if spans[1].Status.Code.String() != "Error" || len(spans[1].Events) == 0 {
		t.Errorf("error harus tercatat di span: %+v", spans[1].Status)
	}

	if got := ExtractTrace(context.Background(), nil); got == nil {
		t.Error("carrier nil harus aman")
	}
}

func TestMiddleware(t *testing.T) {
	buf, exp := setup(t)

	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetActor(r.Context(), "budi", "user-1")
		SetRoute(r.Context(), "GET /api/items/{id}")
		Info(r.Context(), "item.read", "baca item")
		w.WriteHeader(http.StatusNotFound)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/items/42", nil)
	req.Header.Set(HeaderRequestID, "req-abc")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Header().Get(HeaderRequestID) != "req-abc" || rr.Header().Get(HeaderTraceID) == "" {
		t.Errorf("header korelasi harus dikirim: %v", rr.Header())
	}
	lines := logLines(t, buf)
	inner := findEvent(lines, "item.read")
	access := findEvent(lines, "http.request")
	if inner == nil || access == nil {
		t.Fatalf("log kurang: %v", lines)
	}
	for _, l := range []map[string]any{inner, access} {
		if l[KeyRequestID] != "req-abc" || l[KeyActor] != "budi" || l[KeyActorID] != "user-1" {
			t.Errorf("request_id/actor harus ada: %v", l)
		}
	}
	if access[KeyStatus] != float64(404) || access["route"] != "GET /api/items/{id}" || access["level"] != "WARN" {
		t.Errorf("log akses salah: %v", access)
	}
	spans := exp.GetSpans()
	if len(spans) != 1 || spans[0].Name != "GET /api/items/{id}" {
		t.Fatalf("nama span harus pola route: %+v", spans)
	}
}

func TestMiddlewareRejectsBadRequestID(t *testing.T) {
	setup(t)
	h := Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(HeaderRequestID, "bad id\n{injected}")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if got := rr.Header().Get(HeaderRequestID); got == "" || strings.Contains(got, "injected") {
		t.Fatalf("request ID dari client yang aneh harus diganti, dapat %q", got)
	}
}

func TestMiddlewareRecoversPanic(t *testing.T) {
	buf, _ := setup(t)
	h := Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("panic harus jadi 500, dapat %d", rr.Code)
	}
	if findEvent(logLines(t, buf), "http.panic") == nil {
		t.Error("panic harus tercatat di log")
	}
}

func TestSQLTracer(t *testing.T) {
	buf, exp := setup(t)
	tr := &SQLTracer{Database: "target", Level: slog.LevelInfo}

	ctx := tr.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{
		SQL:  "UPDATE items\n   SET price = $1 WHERE id = $2",
		Args: []any{10, 7},
	})
	tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{CommandTag: pgconn.NewCommandTag("UPDATE 3")})

	ctx = tr.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{
		SQL:  "SELECT id FROM users WHERE password_hash = $1",
		Args: []any{"$argon2id$rahasia"},
	})
	tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: &pgconn.PgError{Code: "42P01", Message: "relation does not exist"}})

	ctx = tr.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "commit"})
	tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{CommandTag: pgconn.NewCommandTag("COMMIT")})

	lines := logLines(t, buf)
	var ends []map[string]any
	for _, l := range lines {
		if l[KeyEvent] == "sql.end" {
			ends = append(ends, l)
		}
	}
	if len(ends) != 3 || findEvent(lines, "sql.start") == nil {
		t.Fatalf("mau 3 sql.end dan ada sql.start, dapat: %v", lines)
	}

	ok := ends[0]
	if ok["query"] != "UPDATE items SET price = $1 WHERE id = $2" || ok["rows"] != float64(3) || ok[KeyStatus] != "ok" || ok["duration_ms"] == nil {
		t.Errorf("log query sukses kurang lengkap: %v", ok)
	}
	if params, _ := ok["params"].([]any); len(params) != 2 || params[0] != "10" {
		t.Errorf("parameter harus dicatat: %v", ok["params"])
	}

	failed := ends[1]
	if failed["sqlstate"] != "42P01" || failed["level"] != "ERROR" || failed[KeyStatus] != "error" {
		t.Errorf("query gagal harus bawa SQLSTATE: %v", failed)
	}
	if strings.Contains(buf.String(), "rahasia") {
		t.Error("parameter query yang menyentuh password harus disamarkan")
	}
	if ends[2]["sql_kind"] != "commit" {
		t.Errorf("commit harus tercatat sebagai sql_kind commit: %v", ends[2])
	}

	spans := exp.GetSpans()
	if len(spans) != 3 || spans[1].Status.Code.String() != "Error" {
		t.Fatalf("tiap query satu span, yang gagal berstatus Error: %+v", spans)
	}
}

func TestParseLevel(t *testing.T) {
	for in, want := range map[string]slog.Level{"": slog.LevelInfo, "debug": slog.LevelDebug, "WARN": slog.LevelWarn} {
		got, err := ParseLevel(in)
		if err != nil || got != want {
			t.Errorf("ParseLevel(%q) = %v, %v", in, got, err)
		}
	}
	if _, err := ParseLevel("berisik"); err == nil {
		t.Error("level ngawur harus error")
	}
}

func TestSetupConsoleExporter(t *testing.T) {
	prevLogger, prevTP := slog.Default(), otel.GetTracerProvider()
	t.Cleanup(func() { slog.SetDefault(prevLogger); otel.SetTracerProvider(prevTP) })

	var logs, spans bytes.Buffer
	if err := Setup(context.Background(), Config{Service: "test", LogOut: &logs, TraceOut: &spans}); err != nil {
		t.Fatal(err)
	}
	ctx, span := Start(context.Background(), "contoh.span")
	Info(ctx, "contoh.event", "halo")
	End(span, nil)
	if !strings.Contains(spans.String(), `"Name":"contoh.span"`) || !strings.Contains(spans.String(), `"service.name"`) {
		t.Errorf("span harus ditulis exporter console: %s", spans.String())
	}
	if !strings.Contains(logs.String(), `"service":"test"`) {
		t.Errorf("log harus bawa nama service: %s", logs.String())
	}
	if err := Setup(context.Background(), Config{TraceExporter: "zipkin"}); err == nil {
		t.Error("exporter nggak dikenal harus error")
	}
}
