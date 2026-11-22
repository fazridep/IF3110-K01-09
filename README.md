# Database Management Platform

Tugas Besar IF3110 Milestone 1 dan 2, kelompok K01-09.

Platform buat ngatur akses ke database PostgreSQL bersama. Platform Admin
ngatur user, group, koneksi database, dan grant. User lain bisa lihat schema
dan data, lalu bikin dan jalanin migration sesuai level aksesnya. Semua
migration tercatat di riwayat per database.

Backend dan worker Go, frontend SPA React + Vite yang bicara ke backend lewat
API JSON, database PostgreSQL 18.

## Kebutuhan

| Perangkat | Versi | Cek |
|-----------|-------|-----|
| Go | 1.26 | `go version` |
| Node.js dan npm | 22+ | `node -v` |
| Docker + Docker Compose | Compose v2 | `docker compose version` |
| openssl | apa saja | `openssl version` (buat bikin kunci enkripsi) |

Port yang dipakai:

| Port | Untuk |
|------|-------|
| 8080 | server (API + SPA), bisa diganti lewat `APP_PORT` |
| 5435 | Postgres metadata platform |
| 5436 | Postgres target contoh (`target_db`) |
| 5437 | Postgres khusus test, cuma nyala kalau `make test-db-up` |
| 5173 | Vite dev server, cuma di mode pengembangan |

## Menjalankan

### Pertama kali

```bash
# 1. Konfigurasi
cp .env.example .env
openssl rand -base64 32            # salin hasilnya ke CREDENTIAL_ENCRYPTION_KEY di .env

# 2. Database
make db-up                         # Postgres metadata (5435) dan target contoh (5436)
make migrate                       # bikin semua tabel platform, aman dijalankan berulang

# 3. Akun Platform Admin pertama (ditanya username dan password)
make seed

# 4. Frontend SPA
make frontend-install              # npm ci, sekali atau tiap package-lock.json berubah
make frontend-build                # hasil build di frontend/dist, disajikan server
```

Lalu jalankan server dan worker di **dua terminal terpisah**:

```bash
make run 2> tmp-spans.log          # terminal 1: server API + SPA di http://localhost:8080
make worker 2> tmp-worker-spans.log  # terminal 2: worker yang mengeksekusi job migration
```

Buka http://localhost:8080 dan login pakai akun dari `make seed`.

Log JSON (structured log) keluar di stdout, span OpenTelemetry di stderr. Span
jumlahnya banyak, jadi stderr sebaiknya diarahkan ke file seperti di atas
supaya terminal tetap kebaca. Tanpa worker, job cuma antre dan nggak pernah
jalan.

### Menjalankan lagi

Kalau sudah pernah setup:

```bash
make db-up
make migrate                       # kalau ada migration baru setelah git pull
make frontend-build                # kalau ada perubahan frontend setelah git pull
make run 2> tmp-spans.log          # terminal 1
make worker 2> tmp-worker-spans.log  # terminal 2
```

### Mode pengembangan frontend

Buat ngoprek tampilan dengan hot reload, jalankan `make run` seperti biasa lalu
`make frontend-dev` di terminal lain, dan buka http://localhost:5173. Request
`/api` di-proxy ke server di port 8080. Detailnya di
[docs/frontend.md](docs/frontend.md).

### Menghentikan dan reset

```bash
# Ctrl+C di terminal server dan worker
make db-down                       # matikan container, data tetap ada
docker compose down -v             # matikan dan HAPUS semua data (metadata + target)
```

Setelah `down -v`, ulangi dari `make db-up`, `make migrate`, dan `make seed`.

### Konfigurasi `.env`

| Variabel | Default | Arti |
|----------|---------|------|
| `APP_PORT` | `8080` | port server |
| `APP_ENV` | `development` | `production` bikin cookie `Secure` (wajib HTTPS) |
| `METADATA_DB_URL` | Postgres di 5435 | database metadata platform |
| `CREDENTIAL_ENCRYPTION_KEY` | kosong, **wajib diisi** | kunci AES-GCM (32 byte, base64) buat password koneksi target. Server dan worker harus pakai kunci yang sama. Kalau diganti, koneksi yang sudah tersimpan nggak bisa dibuka |
| `SESSION_*` | 30m, 8h, 15m, 30s | idle timeout, absolute timeout, interval rotasi, grace period (lihat bagian Session) |
| `LOG_LEVEL` | `info` | `debug` ikut mencatat query ke database metadata |
| `OTEL_TRACES_EXPORTER` | `console` | `none` mematikan span |
| `OTEL_SERVICE_NAME` | `db-platform-server` | nama service di log dan span. Kalau baris ini dihapus atau dikosongkan, server tampil sebagai `db-platform-server` dan worker `db-platform-worker`. Kalau diisi, keduanya pakai nama yang sama |
| `SPA_DIR` | `frontend/dist` | folder hasil build SPA |
| `WORKER_CONCURRENCY` | `4` | jumlah job paralel per proses worker (beda database) |
| `WORKER_MAX_ATTEMPTS` | `3` | batas percobaan job yang gagal sementara |
| `WORKER_POLL_INTERVAL` | `1s` | jeda worker mengecek queue |
| `WORKER_LEASE_TTL` | `30s` | lama lock database bertahan kalau worker mati |
| `TEST_DATABASE_URL` | Postgres di 5437 | dipakai `make test` |

### Database target contoh

`docker-compose.yml` sudah menyediakan satu database target. Waktu menambah
koneksi di menu **Koneksi DB**, isi:

| Field | Nilai |
|-------|-------|
| Nama koneksi | bebas, misalnya `toko` |
| Host | `localhost` |
| Port | `5436` |
| Database | `target_db` |
| Username | `target` |
| Password | `target` |
| SSL mode | `disable` |

Isi data contoh supaya explorer nggak kosong:

```bash
docker compose exec db-target psql -U target -d target_db -c \
  "CREATE TABLE produk (id INT PRIMARY KEY, nama TEXT, harga NUMERIC);
   INSERT INTO produk VALUES (1, 'sapu', 15000), (2, 'ember', NULL), (3, 'pel', 25000);"
```

Buat demo mode batch, isi juga tabel besar: `make seed-large` (2 juta row, lihat
bagian Data uji migration besar).

### Kalau ada masalah

| Gejala | Penyebab dan solusi |
|--------|---------------------|
| Server berhenti dengan pesan `CREDENTIAL_ENCRYPTION_KEY` | kunci di `.env` kosong atau bukan base64 32 byte. Bikin dengan `openssl rand -base64 32` |
| Browser menampilkan "Frontend belum di-build" | jalankan `make frontend-build`, lalu refresh |
| Error tabel nggak ada atau kolom nggak dikenal setelah `git pull` | jalankan `make migrate` |
| `pastikan database jalan (make db-up)` | container Postgres belum nyala, jalankan `make db-up` |
| `address already in use` di port 8080 | ganti `APP_PORT` di `.env` |
| Job berhenti di status Antre | worker belum jalan, jalankan `make worker` |
| Tes koneksi gagal "kredensial nggak bisa dibuka" | kunci enkripsi berubah sejak koneksi disimpan. Kembalikan kunci lama, atau ubah koneksi dan isi password lagi |
| Job lama-lama tetap Belum pasti | database target nggak bisa dihubungi. Nyalakan lagi, lalu tekan Periksa ulang di halaman job |

## Skenario demo

Urutannya mengikuti alur minimum spesifikasi: Login → Access Control → Select
Database → View Schema/Data → Create Migration → Validation & Review →
Approval → Queue → Worker → Lock → Revalidation → Execute → Commit/Rollback →
Update Migration History → Audit Log.

**Persiapan (sebagai Platform Admin)**

1. Login, buka **Koneksi DB**, tambah database target contoh, tekan **Tes
   Koneksi**, lalu **Simpan**.
2. **Users**: tambah satu user, misalnya `budi`.
3. **Groups**: bikin dua group supaya approval-nya butuh dua leader.
   - `tim-a`: anggota dan leader akun admin, grant **View + Edit** ke `toko`.
   - `tim-b`: anggota dan leader `budi`, grant **View Only** ke `toko`.

**Explorer**

4. **Databases** → `toko` → tabel `produk`: kolom, constraint, index, dan data
   (NULL ditampilkan miring).

**Migration, approval, queue, worker**

5. Di halaman database, tekan **Migration** → **Migration baru**. Isi SQL
   up `ALTER TABLE produk ADD COLUMN stok INT DEFAULT 0;` dan SQL down
   `ALTER TABLE produk DROP COLUMN stok;`, lalu **Simpan draft**.
6. **Ajukan Apply**. Panel approval menampilkan `0/2 group setuju`.
7. Buka **Approval**, tekan **Setujui**, lalu **Konfirmasi Setuju**. Di jendela
   incognito, login sebagai `budi` dan setujui juga.
8. Kembali ke migration, tekan **Jalankan**. Halaman job langsung terbuka dan
   statusnya berubah sendiri dari Antre → Berjalan → Sukses.

**Gagal, forward, rollback, Hide rollback**

9. Bikin migration kedua dengan SQL up `INSERT INTO pelanggan VALUES (1);` dan
   SQL down `DELETE FROM pelanggan WHERE id = 1;`. Ajukan, setujui dua leader,
   jalankan. Hasilnya **Gagal**, dan riwayatnya mencatat attempt beserta
   error-nya.
10. Bikin tabelnya:
    `docker compose exec db-target psql -U target -d target_db -c "CREATE TABLE pelanggan (id INT)"`.
    Lalu **Ajukan Forward**, setujui, dan jalankan. Hasilnya Sukses.
11. **Ajukan Rollback** untuk migration kedua, setujui, jalankan.
12. Di halaman migration database, centang **Hide rollback**: pasangan forward
    dan rollback-nya hilang dari tampilan, sedangkan attempt yang gagal tetap
    terlihat.

**Akses ditolak**

13. Sebagai `budi` (View Only), buka migration mana pun: nggak ada tombol
    Ajukan, karena cuma bisa melihat.

**Mode batch dan pemulihan worker**

14. Jalankan `make seed-large`, lalu bikin migration mode **Per batch** dengan
    tabel `orders_large`, kolom kursor `id`, ukuran batch `100000`, dan SQL dari
    bagian Data uji migration besar (centang "Simpan data sebelum diubah").
15. Setelah dijalankan, halaman job menampilkan batch dan row yang sudah
    committed. Di tengah jalan, matikan worker (Ctrl+C) lalu jalankan lagi
    `make worker`. Job lanjut dari checkpoint tanpa mengulang batch yang sudah
    committed (lihat bagian Queue, lock, dan pemulihan worker).

**Paralel dan serial**

16. Bikin database kedua di container target:
    `docker compose exec db-target createdb -U target target_db2`, tambahkan
    sebagai koneksi baru (sama seperti `toko`, database `target_db2`), dan beri
    grant ke kedua group. Jalankan migration di dua database bersamaan:
    keduanya Berjalan paralel. Migration di database yang sama selalu urut.

**Log dan trace**

17. Ambil `trace_id` dari log `job.enqueued` di output server, lalu cari di
    output worker. Trace-nya sama dari API sampai query ke database target:
    `grep <trace_id> tmp-spans.log tmp-worker-spans.log`. Daftar event audit
    ada di bagian Audit log.

**Mobile**

18. Buka DevTools mode perangkat (misalnya lebar 390 px): menu jadi tombol
    hamburger, tabel lebar dan editor SQL scroll di dalam komponennya, halaman
    nggak melebar.

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

## Queue, lock, dan pemulihan worker

Setiap eksekusi (apply, forward, rollback, lanjutkan batch) jadi job di tabel
`migration_jobs`. API langsung membalas job ID, lalu worker mengambil job pakai
`SELECT ... FOR UPDATE SKIP LOCKED`, jadi job tetap ada walaupun server atau
worker restart.

**Lock per database.** Waktu mengambil job, worker sekalian memegang lock
database target di tabel `database_locks`, dalam satu transaction. Lock berupa
lease: berlaku `WORKER_LEASE_TTL` (default 30 detik) dan diperpanjang tiap
sepertiganya selama job jalan, termasuk di antara commit batch. Akibatnya:

- satu database cuma menjalankan satu migration, walaupun worker-nya banyak;
- database berbeda diproses paralel (`WORKER_CONCURRENCY` per proses worker);
- job di database yang sama diambil urut (FIFO), dan job berikutnya ditahan
  selama job sebelumnya masih `uncertain` atau `partial`.

Tiap klaim punya token sendiri. Worker yang lease-nya sudah diambil alih nggak
bisa menyimpan hasil lagi, dan eksekusinya langsung dibatalkan.

**Kalau worker mati.** Worker yang mati mendadak (crash, `kill -9`, mesin mati)
nggak sempat melepas lock, jadi lease-nya habis sendiri paling lama
`WORKER_LEASE_TTL` kemudian. Worker lain lalu:

1. menandai job itu `uncertain` dan melepas lock-nya;
2. memeriksa **bukti commit** di database target
   (`_dbplatform.job_commits`). Bukti ini ditulis di transaction yang sama
   dengan SQL migration, jadi ada kalau dan hanya kalau SQL-nya ter-commit;
3. kalau buktinya ada, job langsung `success` tanpa menjalankan SQL lagi.
   Kalau nggak ada, SQL dijalankan ulang sebagai attempt berikutnya. Mode batch
   melanjutkan dari checkpoint terakhir, batch yang sudah committed nggak
   diulang.

Kalau database target nggak bisa dihubungi waktu pemeriksaan, pemeriksaan
diulang dengan jeda sampai `WORKER_MAX_ATTEMPTS` kali. Setelah itu job tetap
`uncertain` dan menahan database itu sampai ada yang menekan **Periksa ulang**
di halaman job. Worker yang dihentikan baik-baik (Ctrl+C) langsung menandai
job-nya `uncertain`, jadi nggak perlu menunggu lease habis.

**Retry dan duplikat.** Kegagalan sementara (koneksi putus, deadlock, server
restart) diulang paling banyak `WORKER_MAX_ATTEMPTS` kali dengan jeda dua kali
lipat tiap percobaan. Error SQL biasa langsung `failed`. Satu approval cuma bisa
jadi satu job, dan primary key bukti commit menolak commit kedua, jadi pesan
duplikat nggak bisa bikin double commit. Semua attempt tercatat di migration
history.

Detail desainnya di ADR-013 dan ADR-015 ([docs/DECISION.md](docs/DECISION.md)).

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
