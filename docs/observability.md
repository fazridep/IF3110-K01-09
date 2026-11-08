# Logging dan Tracing

Log JSON (`log/slog`) ke stdout, span OpenTelemetry ke stderr. Kode di
`internal/platform/obs`.

| Env | Default |
|-----|---------|
| `LOG_LEVEL` | `info` |
| `OTEL_TRACES_EXPORTER` | `console` (atau `none`) |
| `OTEL_SERVICE_NAME` | `db-platform-server` |

```bash
make run > logs.jsonl 2> spans.jsonl
```

## Atribut log

`time`, `level`, `msg`, `service`, `event`, `actor`, `actor_id`,
`request_id`, `trace_id`, `span_id`, plus `database`, `status`,
`migration_id`, `job_id`, `execution_id`, `attempt`, `batch` kalau relevan.
Password, token, sid, cookie, dan DSN otomatis jadi `[REDACTED]`.

## Contoh output

**Log (stdout)**:

```json
{"time":"2026-11-06T14:21:50.079027+07:00","level":"WARN","msg":"login gagal","service":"db-platform-server","event":"auth.login","actor":"admin","status":"invalid_credentials","trace_id":"007206c5f17f273103fd96da77ab37a2","span_id":"f5dab778cc0c0391","request_id":"vxz3rvgk2nbqn4viwfhuzujavm"}
{"time":"2026-11-06T14:21:50.116601+07:00","level":"INFO","msg":"login berhasil","service":"db-platform-server","event":"auth.login","status":"ok","trace_id":"1c8cf51a35b2e5400a9800a687570db8","span_id":"1fa83022062987ef","request_id":"nlb3tx57ixuzlzqxtvt7k4x55g","actor":"admin","actor_id":"161be72d-7d87-4a8c-ae08-259bc4a76dfd"}
{"time":"2026-11-06T14:21:50.116742+07:00","level":"INFO","msg":"request selesai","service":"db-platform-server","event":"http.request","method":"POST","path":"/api/session","route":"POST /api/session","status":201,"duration_ms":30.118,"bytes":88,"trace_id":"1c8cf51a35b2e5400a9800a687570db8","span_id":"1fa83022062987ef","request_id":"nlb3tx57ixuzlzqxtvt7k4x55g","actor":"admin","actor_id":"161be72d-7d87-4a8c-ae08-259bc4a76dfd"}
{"time":"2026-11-06T14:21:50.133126+07:00","level":"WARN","msg":"request ditolak CSRF","service":"db-platform-server","event":"auth.csrf_rejected","method":"DELETE","path":"/api/session","trace_id":"fefdee84dccfff5a31ff2e581436ec4f","span_id":"a19e3eda04063445","request_id":"zldsfduwuv4n75fhvywn35zgmn"}
```

**Span (stderr)**:

```json
{
  "Name": "POST /api/session",
  "SpanContext": { "TraceID": "007206c5f17f273103fd96da77ab37a2", "SpanID": "f5dab778cc0c0391", "TraceFlags": "01" },
  "Parent": { "TraceID": "00000000000000000000000000000000", "SpanID": "0000000000000000" },
  "SpanKind": 2,
  "StartTime": "2026-11-06T14:21:50.042597+07:00",
  "EndTime": "2026-11-06T14:21:50.079211166+07:00",
  "Attributes": [
    { "Key": "http.request.method", "Value": { "Type": "STRING", "Value": "POST" } },
    { "Key": "url.path", "Value": { "Type": "STRING", "Value": "/api/session" } },
    { "Key": "http.request_id", "Value": { "Type": "STRING", "Value": "vxz3rvgk2nbqn4viwfhuzujavm" } },
    { "Key": "http.route", "Value": { "Type": "STRING", "Value": "POST /api/session" } },
    { "Key": "http.response.status_code", "Value": { "Type": "INT64", "Value": 401 } }
  ],
  "Status": { "Code": "Unset" },
  "Resource": [{ "Key": "service.name", "Value": { "Type": "STRING", "Value": "db-platform-server" } }]
}
```

`trace_id` di log dan `TraceID` di span sama, jadi bisa dicocokkan:

```bash
grep '007206c5f17f273103fd96da77ab37a2' logs.jsonl spans.jsonl
```

## Trace job migration

Job membawa trace context dari request yang memasukkannya, disimpan di kolom
`trace_context`. Jadi satu trace menyambung dari API sampai SQL di database target:

```
POST /api/jobs -> queue.enqueue -> worker.job -> lock.acquire
                                              -> db.begin, db.query, db.commit
```

Event worker: `lock.acquired`, `lock.released`, `lock.expired`, `lock.lost`,
`job.enqueued`, `job.claimed` (ada `queue_wait_ms`), `job.attempt.start`,
`job.attempt.end` (ada `duration_ms`), `job.retry`, `job.verified`,
`job.progress`, `job.finished`.

Semua SQL ke database target masuk log level info (`sql.start`, `sql.end`).
Query metadata di server level debug, dan polling worker nggak dicatat supaya
log nggak penuh.
