#!/usr/bin/env bash
# Stress test k6 (bonus). Bikin database metadata dan beberapa database target
# kosong di container db-test, menjalankan server + worker dari kode sekarang,
# menjalankan loadtest/stress.js lewat image grafana/k6, lalu mengecek bahwa nggak
# ada double commit. Semua database uji dihapus lagi.
#   STAGE_SECONDS=60 TARGETS=8 ROWS=20000 ./scripts/loadtest.sh
set -euo pipefail
cd "$(dirname "$0")/.."

PORT=${LOAD_PORT:-18091}
TARGETS=${TARGETS:-8}
ROWS=${ROWS:-20000}
STAGE_SECONDS=${STAGE_SECONDS:-60}
META=load_meta
OUT=tmp/loadtest
mkdir -p "$OUT" loadtest/results
psql() { docker compose exec -T db-test psql -U test -v ON_ERROR_STOP=1 -q "$@"; }

names=()
for i in $(seq 1 "$TARGETS"); do names+=("load_t$i"); done

docker compose up -d --wait db-test >/dev/null
for db in "$META" "${names[@]}"; do
  psql -d platform_test -c "DROP DATABASE IF EXISTS $db" -c "CREATE DATABASE $db" 2>/dev/null
done
for db in "${names[@]}"; do
  psql -d "$db" -c "CREATE TABLE load_items (id INT PRIMARY KEY, counter INT NOT NULL DEFAULT 0);
    INSERT INTO load_items (id) SELECT generate_series(1, $ROWS); ANALYZE load_items;"
done

export METADATA_DB_URL="postgres://test:test@localhost:5437/$META?sslmode=disable"
export CREDENTIAL_ENCRYPTION_KEY=$(openssl rand -base64 32)
export APP_PORT=$PORT LOG_LEVEL=info OTEL_TRACES_EXPORTER=console SPA_DIR=/nonexistent
export WORKER_CONCURRENCY=${WORKER_CONCURRENCY:-4} WORKER_POLL_INTERVAL=200ms WORKER_LEASE_TTL=10s
ADMIN_PASSWORD=admin-load-123

go build -o "$OUT/server" ./cmd/server
go build -o "$OUT/worker" ./cmd/worker
"$OUT/server" migrate >/dev/null 2>&1
printf 'admin\n%s\n' "$ADMIN_PASSWORD" | "$OUT/server" seed-admin >/dev/null

"$OUT/server" >"$OUT/server.log" 2>"$OUT/server-spans.log" & SERVER=$!
"$OUT/worker" >"$OUT/worker.log" 2>"$OUT/worker-spans.log" & WORKER=$!
cleanup() {
  kill "$SERVER" "$WORKER" 2>/dev/null || true
  wait "$SERVER" "$WORKER" 2>/dev/null || true
  for db in "$META" "${names[@]}"; do psql -d platform_test -c "DROP DATABASE IF EXISTS $db" 2>/dev/null || true; done
}
trap cleanup EXIT
for _ in $(seq 1 50); do curl -sf "http://localhost:$PORT/api/health" >/dev/null && break; sleep 0.2; done

status=0
docker run --rm --network host -u "$(id -u):$(id -g)" -v "$PWD/loadtest:/work" -w /work \
  -e BASE_URL="http://localhost:$PORT" -e ADMIN_PASSWORD="$ADMIN_PASSWORD" \
  -e TARGET_DBS="$(IFS=,; echo "${names[*]}")" -e STAGE_SECONDS="$STAGE_SECONDS" \
  grafana/k6:1.4.2 run --quiet stress.js || status=$?

# Integritas: nilai counter di tiap target harus sama dengan jumlah job apply yang
# sukses ke database itu. Kalau ada double commit, nilainya lebih besar.
echo "--- cek double commit"
bad=0
for db in "${names[@]}"; do
  jobs=$(psql -d "$META" -tA -c "SELECT count(*) FROM migration_jobs j JOIN database_connections c ON c.id = j.database_id WHERE c.name = '$db' AND j.status = 'success'")
  counter=$(psql -d "$db" -tA -c "SELECT DISTINCT counter FROM load_items")
  echo "$db: $jobs job sukses, counter $counter"
  [ "$jobs" = "$counter" ] || bad=1
done
[ "$bad" = 0 ] && echo "nggak ada double commit" || { echo "DOUBLE COMMIT TERDETEKSI"; status=1; }
exit "$status"
