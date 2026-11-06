package obs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// SQLTracer nyatat setiap query sebagai log sql.start, sql.end, dan satu span.
type SQLTracer struct {
	Database string
	Level    slog.Level
}

// InstrumentPool dipanggil sebelum pgxpool.NewWithConfig.
func InstrumentPool(cfg *pgxpool.Config, database string, level slog.Level) {
	cfg.ConnConfig.Tracer = &SQLTracer{Database: database, Level: level}
}

type queryState struct {
	start time.Time
	sql   string
	args  []string
	span  trace.Span
}

func (t *SQLTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	sql := RedactString(compactSQL(data.SQL))
	kind := sqlKind(sql)
	ctx, span := StartKind(ctx, "db."+kind, trace.SpanKindClient,
		attribute.String("db.system.name", "postgresql"),
		attribute.String("db.namespace", t.Database),
		attribute.String("db.query.text", truncate(sql, 2000)),
	)
	args := formatArgs(data.SQL, data.Args)
	Event(ctx, t.Level, "sql.start", "query mulai",
		KeyDatabase, t.Database, "sql_kind", kind, "query", truncate(sql, 4000), "params", args)
	return context.WithValue(ctx, ctxQuery, &queryState{start: time.Now(), sql: sql, args: args, span: span})
}

func (t *SQLTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	st, ok := ctx.Value(ctxQuery).(*queryState)
	if !ok {
		return
	}
	durationMs := float64(time.Since(st.start).Microseconds()) / 1000
	kind := sqlKind(st.sql)
	attrs := []any{
		KeyDatabase, t.Database,
		"sql_kind", kind,
		"query", truncate(st.sql, 4000),
		"params", st.args,
		"duration_ms", durationMs,
	}
	if data.Err == nil {
		rows := data.CommandTag.RowsAffected()
		st.span.SetAttributes(attribute.Int64("db.response.returned_rows", rows))
		Event(ctx, t.Level, "sql.end", "query selesai", append(attrs, KeyStatus, "ok", "rows", rows, "command", data.CommandTag.String())...)
		End(st.span, nil)
		return
	}

	var pgErr *pgconn.PgError
	if errors.As(data.Err, &pgErr) {
		attrs = append(attrs, "sqlstate", pgErr.Code)
		st.span.SetAttributes(attribute.String("db.response.status_code", pgErr.Code))
	}
	level := slog.LevelError
	if errors.Is(data.Err, context.Canceled) {
		level = slog.LevelWarn
	}
	Event(ctx, level, "sql.end", "query gagal", append(attrs, KeyStatus, "error", "error", data.Err.Error())...)
	End(st.span, data.Err)
}

var spaces = regexp.MustCompile(`\s+`)

func compactSQL(sql string) string {
	return strings.TrimSpace(spaces.ReplaceAllString(sql, " "))
}

func sqlKind(sql string) string {
	first, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(sql)), " ")
	first = strings.TrimSuffix(first, ";")
	switch first {
	case "begin", "start":
		return "begin"
	case "commit", "end":
		return "commit"
	case "rollback", "abort":
		return "rollback"
	case "savepoint", "release":
		return "savepoint"
	}
	return "query"
}

var sensitiveSQL = regexp.MustCompile(`(?i)password|token|secret|credential`)

func formatArgs(sql string, args []any) []string {
	if len(args) == 0 {
		return nil
	}
	out := make([]string, len(args))
	hide := sensitiveSQL.MatchString(sql)
	for i, a := range args {
		switch v := a.(type) {
		case nil:
			out[i] = "NULL"
		case []byte:
			out[i] = fmt.Sprintf("[bytes:%d]", len(v))
		default:
			if hide {
				out[i] = Redacted
				continue
			}
			out[i] = truncate(RedactString(fmt.Sprint(v)), 200)
		}
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}