# Database Management Platform

Tugas Besar IF3110 Milestone 1 dan 2, kelompok K01-09.

Platform buat ngatur akses ke database PostgreSQL bersama. Platform Admin
ngatur user, group, koneksi database, dan grant. User lain bisa lihat schema
dan data, lalu bikin dan jalanin migration sesuai level aksesnya. Semua
migration tercatat di riwayat per database.

Backend dan worker Go, frontend SPA React + Vite yang bicara ke backend lewat
API JSON, database PostgreSQL 18.

## Kebutuhan

- Go 1.26
- Node.js 22+ dan npm
- Docker dengan Docker Compose

## Menjalankan

```bash
cp .env.example .env
# isi CREDENTIAL_ENCRYPTION_KEY, bikin pakai: openssl rand -base64 32
make db-up        # postgres metadata (5435) dan target (5436)
make migrate      # bikin tabel platform
make seed         # bikin akun Platform Admin pertama
make frontend-install
make frontend-build
make run          # http://localhost:8080
make worker       # terminal lain, yang menjalankan job migration
```

Kalau port 8080 sudah dipakai aplikasi lain, ganti `APP_PORT` di `.env`.

`docker-compose.yml` sudah nyediain satu database target buat dicoba. Waktu
nambah koneksi di menu Koneksi, isi:

| Field | Nilai |
|-------|-------|
| Host | `localhost` |
| Port | `5436` |
| Database | `target_db` |
| Username | `target` |
| Password | `target` |
| sslmode | `disable` |

## Alur pakai

1. Login sebagai Platform Admin.
2. Koneksi: tambah database target, lalu tes koneksinya.
3. Users: tambah user biasa.
4. Groups: bikin group, masukin anggota, lalu kasih akses ke database
   (View Only atau View + Edit).
5. Login sebagai user tadi. Di Databases kelihatan database yang boleh dibuka,
   lengkap dengan schema, tabel, kolom, constraint, index, dan datanya.
6. Migration: bikin draft berisi SQL up dan SQL down (atau tandai
   irreversible beserta alasannya), pilih mode normal atau per batch.
7. Ajukan approval. Leader dari semua group yang punya akses ke database itu
   harus setuju, termasuk untuk rollback dan forward.
8. Setelah disetujui, tekan Jalankan. Job masuk queue, worker yang
   mengeksekusi, dan progresnya kelihatan di halaman job.
9. Riwayat eksekusi ada di halaman migration. Centang Hide rollback buat
   menyembunyikan eksekusi yang sudah dibatalkan beserta rollback-nya.

## Hak akses

| Peran | Bisa apa |
|-------|----------|
| Platform Admin | kelola user, group, koneksi, dan grant |
| View Only | lihat schema, data, dan riwayat migration |
| View + Edit | semua View Only, plus bikin dan ubah draft, ajukan approval, dan jalanin migration yang sudah disetujui |

- Akses database cuma dikasih lewat group. Kalau user masuk beberapa group yang
  punya akses ke database yang sama, level tertinggi yang dipakai.
- Platform Admin bukan level akses database. Kalau admin mau buka database,
  dia harus masuk group dulu kayak user lain.
- Semua pengecekan izin ada di `internal/access` dan dicek di backend, bukan
  cuma disembunyiin di tampilan.

## Session

| Parameter | Nilai | Env |
|-----------|-------|-----|
| Idle timeout | 30 menit | `SESSION_IDLE_TIMEOUT` |
| Absolute expiration | 8 jam sejak login | `SESSION_ABSOLUTE_TIMEOUT` |
| Interval rotasi | 15 menit | `SESSION_ROTATION_INTERVAL` |
| Grace period token lama | 30 detik | `SESSION_GRACE_PERIOD` |

Session disimpan di server, client cuma pegang token acak di cookie. Token
dirotasi waktu login, tiap 15 menit, dan waktu privilege naik. Logout, cabut
admin, keluarin user dari group, atau nonaktifkan user langsung mencabut
session-nya di server. Password di-hash pakai argon2id. Detailnya ada di
ADR-003 dan ADR-004.

## Tes

```bash
make test-db-up   # postgres khusus tes (5437)
make test
```

Kalau cuma mau unit test tanpa database, jalankan `go test ./...`. Integration
test otomatis di-skip kalau `TEST_DATABASE_URL` kosong.

Test frontend (Vitest + Testing Library):

```bash
make frontend-test
```

Test end-to-end lewat browser (Playwright, Chromium):

```bash
cd frontend && npx playwright install chromium   # sekali saja
make e2e
```

`scripts/e2e.sh` membuat database metadata dan target kosong di container
`db-test`, menjalankan server dan worker dari kode sekarang, lalu menjalankan
`frontend/e2e`. Skenarionya mengikuti alur minimum spesifikasi:

- admin membuat koneksi, user, dan dua group yang masing-masing punya leader
- explorer menampilkan schema, tabel, dan data (termasuk NULL)
- migration diajukan, disetujui leader kedua group, masuk queue, dan
  dijalankan worker
- apply yang gagal tercatat di riwayat, lalu di-forward setelah penyebabnya
  dibereskan, di-rollback, dan Hide rollback menyembunyikan pasangannya
- leader dengan akses View Only nggak bisa mengajukan perubahan (`403`)
- halaman utama nggak melebar di layar 390px

Setelah itu log JSON-nya diperiksa: tiap event worker punya `actor`, trace dari
API nyambung ke worker, SQL ke database target tercatat, dan password nggak
pernah muncul. Laporan dan trace kegagalan ada di `frontend/playwright-report`.

## Audit log

Audit log berupa structured log JSON ke stdout dari server dan worker, bukan
tabel di database. Bagian Activity Logging spesifikasi menyebut log "tidak
disimpan di database platform". Jejak perubahan database yang harus persisten
sudah ada di migration history, yang append-only dan mencatat setiap attempt.
Detailnya di ADR-016.

| Area | Event |
|------|-------|
| Login dan session | `auth.login`, `auth.logout`, `auth.session_invalid`, `auth.csrf_rejected` |
| Admin | `admin.user_created`, `admin.user_admin_updated`, `admin.user_active_updated`, `admin.group_created`, `admin.group_updated`, `admin.group_deleted`, `admin.group_leader_set`, `admin.member_added`, `admin.member_removed`, `admin.grant_set`, `admin.grant_revoked` |
| Koneksi database | `connection.created`, `connection.updated`, `connection.deleted`, `connection.test_failed` |
| Approval | `approval.submitted`, `approval.approved`, `approval.rejected` |
| Migration | `migration.created`, `migration.updated`, `migration.execute.start`, `migration.execute.result`, `migration.commit`, `migration.rollback`, `migration.batch.commit`, `migration.batch.rollback` |
| Queue dan worker | `job.enqueued`, `job.claimed`, `job.attempt.start`, `job.attempt.end`, `job.retry`, `job.verified`, `job.finished`, `lock.acquired`, `lock.released`, `lock.expired` |
| SQL | `sql.start` dan `sql.end` untuk setiap query ke database target, termasuk yang read-only |

Tiap event membawa `actor`, `trace_id`, dan `request_id` (di worker: `job_id`
dan `execution_id`). Contoh dan cara mencocokkan log dengan span ada di
[docs/observability.md](docs/observability.md).

## Data uji migration besar

`make seed-large` membuat tabel `orders_large` di database target lalu mengukur
satu `UPDATE` seluruh row tanpa batching (`ROWS=5000000 make seed-large` buat
mengganti jumlahnya). Hasil 8 November 2026, Postgres 18 di Docker, laptop
pengembang:

| Row | Isi data | `UPDATE` seluruh row tanpa batching |
|-----|----------|-------------------------------------|
| 2.000.000 | 3,4 detik | 6,7 detik |

Dataset yang sama dipakai buat menguji migration per batch. Bikin migration
mode batch di database target dengan tabel `orders_large`, kolom kursor `id`,
dan ukuran batch misalnya 10000:

```sql
-- SQL up
UPDATE orders_large o SET amount = o.amount * 1.1
FROM batch_rows b WHERE o.id = b.id;

-- SQL down (centang "Simpan data sebelum diubah")
UPDATE orders_large o SET amount = b.amount
FROM batch_rows b WHERE o.id = b.id;
```

`batch_rows` itu temp table berisi row satu batch yang disiapkan worker. Tiap
batch di-commit sendiri bareng checkpoint-nya, jadi kalau berhenti di tengah,
statusnya partial dan bisa dilanjutkan dari batch berikutnya. Detailnya di
ADR-015.

Hasil 9 November 2026 di dataset yang sama (2.000.000 row, batch 100.000, data
pemulihan disimpan), lewat queue dan worker:

| Eksekusi | Batch | Durasi |
|----------|-------|--------|
| apply | 20 | 33,5 detik |
| rollback per batch | 20 | 30,9 detik |

Lebih lama dari `UPDATE` sekali jalan karena tiap batch menyalin row ke
`batch_rows` dan menyimpan isinya sebagai data pemulihan. Gantinya, tiap batch
bisa di-commit, dilanjutkan, dan dibatalkan sendiri-sendiri.

## Stress test k6 (bonus)

```bash
make test-db-up
make loadtest            # STAGE_SECONDS=60 TARGETS=8 ROWS=20000 bisa diubah
```

`scripts/loadtest.sh` membuat database metadata dan 8 database target kosong
(masing-masing tabel `load_items` 20.000 row), menjalankan server dan worker
(`WORKER_CONCURRENCY=4`) dari kode sekarang, lalu menjalankan
[`loadtest/stress.js`](loadtest/stress.js) lewat image `grafana/k6:1.4.2`.

### Isi script

Dua skenario `ramping-vus` jalan bersamaan:

- **api**: tiap iterasi membaca `GET /api/databases`, detail database, isi
  tabel, `GET /api/jobs`, dan riwayat migration, lalu jeda 1 detik.
- **migrations**: tiap iterasi membuat migration
  (`UPDATE load_items SET counter = counter + 1`), mengajukan dan menyetujui
  approval, lalu mengirim `POST /api/jobs` **3 kali serentak dengan approval
  yang sama** (pengiriman job bersamaan ke database yang sama). Ketiganya harus
  dapat job ID yang sama. Setelah itu job dipantau lewat `GET /api/jobs/{id}`
  sampai final. Yang dihitung sukses adalah status final job, bukan respons
  `202`. Database dipilih bergiliran, jadi VU yang jalan bareng mengirim job ke
  database berbeda dan worker memprosesnya paralel.

Beban normal yang ditetapkan tim adalah 5 user membaca API dan 2 alur
migration bersamaan. Tiap tahap 60 detik (15 detik naik, 45 detik ditahan):
normal → tinggi (4x) → puncak (10x baca, 8 alur migration) → pemulihan
(kembali ke normal).

Threshold:

| Metrik | Batas |
|--------|-------|
| `http_req_duration{kind:api}` p95 | < 500 ms |
| `http_req_duration{kind:submit}` p95 (kirim job) | < 1000 ms |
| `http_req_failed` | < 1% |
| `job_success` (status final job) | > 99% |
| `job_duplicate_mismatch` (kiriman duplikat beda job) | 0 |

Metrik tambahan: `job_queue_wait_ms` (job masuk queue sampai diambil worker),
`job_duration_ms` (diambil sampai selesai), `job_end_to_end_ms`, dan
`jobs_done`. Semuanya ditandai per tahap. Setelah k6 selesai, skrip mengecek
nilai `counter` di tiap database target sama dengan jumlah job sukses ke
database itu. Kalau ada double commit, nilainya pasti lebih besar.

### Hasil

19 November 2026, laptop i7-12700H (20 thread, 15 GB), Postgres 18 di Docker,
server dan worker di mesin yang sama, log level info dengan exporter span
console. Hasil lengkap di [`loadtest/results/summary.md`](loadtest/results/summary.md).

| Tahap | VU (baca + migration) | API p50 / p95 (ms) | Request/detik | Error | Kirim job p95 (ms) | Job selesai | Job sukses | Tunggu queue p50 / p95 (ms) | Durasi job p50 / p95 (ms) |
|---|---|---|---|---|---|---|---|---|---|
| normal | 5 + 2 | 3,5 / 12,9 | 42,7 | 0% | 5,6 | 186 | 100% | 35 / 139 | 70 / 99 |
| tinggi | 20 + 4 | 4,0 / 12,0 | 136,6 | 0% | 6,8 | 415 | 100% | 31 / 144 | 63 / 82 |
| puncak | 50 + 8 | 5,9 / 15,2 | 321,5 | 0% | 10,3 | 832 | 100% | 82 / 158 | 80 / 109 |
| pemulihan | 5 + 2 | 5,4 / 12,7 | 90,3 | 0% | 7,6 | 334 | 100% | 50 / 145 | 70 / 93 |

Total 35.602 request dan 1.767 job (sekitar 14 job/detik di puncak), semua
threshold lolos, 0 kiriman duplikat yang menghasilkan job berbeda, dan cek
integritas menunjukkan 221 job sukses = counter 221 di tiap database (220 di
satu database). Nggak ada double commit.

### Analisis per tingkat beban

- **Normal.** API p95 12,9 ms, jauh di bawah threshold 500 ms. p95 sedikit
  lebih tinggi daripada tahap tinggi karena beberapa request pertama lebih
  lambat (`GET /api/databases` p95 27 ms di tahap ini, 7,7 ms di tahap tinggi),
  kemungkinan karena pool koneksi ke database target baru dibuat. Login nggak
  ikut dihitung, request-nya ditandai `setup`.
- **Tinggi (4x).** Throughput naik 3x dengan p50 hampir sama. Server belum
  jenuh. Tunggu queue tetap sekitar 30 ms (p50) karena job baru masuk lebih
  jarang dari kapasitas 4 worker paralel.
- **Puncak (10x baca, 8 migration).** Throughput 321 request/detik, p50 naik
  dari 3,5 ke 5,9 ms dan p95 ke 15,2 ms, error tetap 0%. Tunggu queue p50 naik
  ke 82 ms: ada 8 alur migration tapi cuma 4 slot worker, jadi sebagian job
  antre. Tunggu queue p95 di semua tahap sekitar 140–160 ms, didominasi
  interval polling worker 200 ms, bukan lock. Durasi job naik sedikit (p50 70
  → 80 ms) karena sampai 4 `UPDATE` jalan paralel di Postgres yang sama.
- **Pemulihan.** Job dan kirim job kembali ke angka normal. API p50 masih 5,4
  ms karena datanya membesar, bukan karena server belum pulih. Dari log server,
  endpoint yang ukuran datanya tetap sudah kembali seperti awal
  (`GET /api/databases/{id}` p50 3,1 → 3,0 ms, `GET /api/jobs/{id}` 2,2 → 2,2
  ms), sedangkan `GET /api/databases/{id}/history` (2,3 → 6,2 ms) dan
  `GET /api/jobs` (2,6 → 5,5 ms) lebih lambat karena tiap database sekarang
  punya 220-an eksekusi. Request/detik dan jumlah job di tahap ini juga lebih
  tinggi dari normal karena 15 detik pertamanya masih turun dari puncak, dan
  job yang selesai di masa tenggang setelah tahap terakhir dihitung di sini.

Catatan dan saran:

- Riwayat migration dikirim utuh tanpa paging. Dengan ratusan eksekusi per
  database latensinya mulai naik, jadi perlu paging kalau datanya jauh lebih besar.
- Exporter span console menulis sinkron dan menghasilkan sekitar 500 MB span
  dalam 5 menit uji ini. Untuk beban tinggi lebih cocok exporter OTLP dengan
  batching (bonus observability).
- Tunggu queue bisa dikurangi dengan `WORKER_POLL_INTERVAL` lebih kecil atau
  menambah proses worker. Lock per database tetap menjaga satu migration per
  database.

## Struktur

```
cmd/server/            entry point API: migrate, seed-admin, wiring server
cmd/worker/            entry point worker: menjalankan job migration dari queue
frontend/              SPA React + Vite
internal/              kode backend
  auth/                login, session, CSRF
  access/              aturan izin
  admin/               user, group, leader, dan grant
  approval/            persetujuan leader group
  dbmgmt/              koneksi database target dan viewer
  migration/           migration database target
  queue/               job queue, worker, lock per database, retry, pemulihan
  web/                 router API dan penyaji SPA
  platform/            pondasi tanpa logika fitur, nggak boleh import fitur
    config/ httpx/ obs/ pgerr/ store/ testutil/
migrations/metadata/   SQL tabel platform
scripts/               data uji migration besar, e2e, dan stress test
loadtest/              script k6 dan hasil stress test
docs/                  keputusan desain, API, frontend, observability, progress
```

## Dokumentasi

- [Progress dan pembagian tugas](docs/PROGRESS.md)
- [Konvensi dan daftar API](docs/api.md)
- [Frontend SPA](docs/frontend.md)
- [Logging dan tracing, contoh output log dan span](docs/observability.md)
- [Keputusan desain](docs/DECISION.md)
- [Aturan migration metadata](migrations/README.md)
