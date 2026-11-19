#!/usr/bin/env bash
# Uji end-to-end lewat browser (Playwright). Bikin database metadata dan target
# kosong di container db-test, menjalankan server + worker dari kode sekarang,
# menjalankan frontend/e2e, lalu memeriksa log JSON-nya. Semua dibersihkan lagi.
set -euo pipefail
cd "$(dirname "$0")/.."

PORT=${E2E_PORT:-18090}
META=e2e_meta
TARGET=e2e_target
OUT=tmp/e2e
mkdir -p "$OUT"
psql() { docker compose exec -T db-test psql -U test -v ON_ERROR_STOP=1 -q "$@"; }

docker compose up -d --wait db-test >/dev/null
psql -d platform_test -c "DROP DATABASE IF EXISTS $META" -c "CREATE DATABASE $META" \
     -c "DROP DATABASE IF EXISTS $TARGET" -c "CREATE DATABASE $TARGET" 2>/dev/null
psql -d "$TARGET" -c "CREATE TABLE produk (id INT PRIMARY KEY, nama TEXT); INSERT INTO produk VALUES (1, 'sapu'), (2, NULL);"

export METADATA_DB_URL="postgres://test:test@localhost:5437/$META?sslmode=disable"
export CREDENTIAL_ENCRYPTION_KEY=$(openssl rand -base64 32)
export APP_PORT=$PORT SPA_DIR=frontend/dist LOG_LEVEL=info OTEL_TRACES_EXPORTER=console
export WORKER_POLL_INTERVAL=200ms WORKER_LEASE_TTL=5s
export E2E_BASE_URL="http://localhost:$PORT" E2E_TARGET_DB=$TARGET E2E_ADMIN_PASSWORD=admin-e2e-123

go build -o "$OUT/server" ./cmd/server
go build -o "$OUT/worker" ./cmd/worker
(cd frontend && npm run build >/dev/null)
"$OUT/server" migrate >/dev/null 2>&1
printf 'admin\n%s\n' "$E2E_ADMIN_PASSWORD" | "$OUT/server" seed-admin >/dev/null

"$OUT/server" >"$OUT/server.log" 2>"$OUT/server-spans.log" & SERVER=$!
"$OUT/worker" >"$OUT/worker.log" 2>"$OUT/worker-spans.log" & WORKER=$!
cleanup() {
  kill "$SERVER" "$WORKER" 2>/dev/null || true
  wait "$SERVER" "$WORKER" 2>/dev/null || true
  psql -d platform_test -c "DROP DATABASE IF EXISTS $META" -c "DROP DATABASE IF EXISTS $TARGET" 2>/dev/null || true
}
trap cleanup EXIT
for _ in $(seq 1 50); do curl -sf "$E2E_BASE_URL/api/health" >/dev/null && break; sleep 0.2; done

(cd frontend && npx playwright test "$@")

# Log: tiap event worker membawa actor, trace dari API nyambung ke worker, dan
# password nggak pernah muncul.
python3 - "$OUT" "$E2E_ADMIN_PASSWORD" <<'PY'
import json, sys
out, password = sys.argv[1], sys.argv[2]
def load(name):
    return [json.loads(l) for l in open(f"{out}/{name}") if l.startswith("{")]
server, worker = load("server.log"), load("worker.log")
jobs = [r for r in worker if r.get("job_id")]
assert jobs, "worker nggak mencatat job"
missing = [r["event"] for r in jobs if not r.get("actor")]
assert not missing, f"event worker tanpa actor: {set(missing)}"
enqueued = {r["trace_id"] for r in server if r.get("event") == "job.enqueued"}
claimed = {r["trace_id"] for r in worker if r.get("event") == "job.claimed"}
assert enqueued and enqueued <= claimed, "trace API -> worker nggak nyambung"
sql = [r for r in worker if r.get("event") == "sql.end" and r.get("job_id")]
assert sql, "SQL target dari worker nggak tercatat"
for name in ("server.log", "worker.log", "server-spans.log", "worker-spans.log"):
    assert password not in open(f"{out}/{name}").read(), f"password bocor di {name}"
print(f"log ok: {len(jobs)} event job, {len(enqueued)} trace API->worker, {len(sql)} query target, tanpa password")
PY
