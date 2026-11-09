# Database Management Platform

Tugas Besar IF3110 Milestone 1, kelompok K01-09.

Platform buat ngatur akses ke database PostgreSQL bersama. Platform Admin
ngatur user, group, koneksi database, dan grant. User lain bisa lihat schema
dan data, lalu bikin dan jalanin migration sesuai level aksesnya. Semua
migration tercatat di riwayat per database.

Backend Go, tampilan pakai `html/template` + Pico.css (bukan SPA), database
PostgreSQL 18.

## Kebutuhan

- Go 1.26
- Docker dengan Docker Compose

## Menjalankan

```bash
cp .env.example .env
# isi CREDENTIAL_ENCRYPTION_KEY, bikin pakai: openssl rand -base64 32
make db-up        # postgres metadata (5435) dan target (5436)
make migrate      # bikin tabel platform
make seed         # bikin akun Platform Admin pertama
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
  web/                 router, penyaji SPA, template yang belum pindah ke SPA
  platform/            pondasi tanpa logika fitur, nggak boleh import fitur
    config/ httpx/ obs/ pgerr/ store/ testutil/
migrations/metadata/   SQL tabel platform
scripts/               data uji migration besar
docs/                  keputusan desain, API, frontend, observability, progress
```

## Dokumentasi

- [Progress dan pembagian tugas](docs/PROGRESS.md)
- [Konvensi dan daftar API](docs/api.md)
- [Keputusan desain](docs/DECISION.md)
- [Aturan migration metadata](migrations/README.md)
