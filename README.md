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
6. Riwayat migration: bikin migration, tinjau SQL-nya, lalu jalankan. Kalau
   sukses di-commit, kalau gagal di-rollback dan errornya tetap dicatat.

## Hak akses

| Peran | Bisa apa |
|-------|----------|
| Platform Admin | kelola user, group, koneksi, dan grant |
| View Only | lihat schema, data, dan riwayat migration |
| View + Edit | semua View Only, plus bikin, ubah, dan jalanin migration |

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

Dataset yang sama dipakai buat menguji migration per batch.

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
