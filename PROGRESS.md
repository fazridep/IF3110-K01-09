# Progress Tubes IF3110 - Database Management Platform

Terakhir diupdate: **19 Oktober 2026** (pembagian tugas)

Deadline Milestone 1: 25 Oktober 2026, 13.10 WIB.

## Pembagian

| Bagian | Orang | Folder |
|--------|-------|--------|
| 1. Fondasi & Login | Fazri | `cmd/server`, `internal/auth`, `internal/web`, `internal/store` |
| 2. Izin & Admin | Zahran | `internal/access`, `internal/admin` |
| 3. Lihat Database | Dzakwan | `internal/dbmgmt` |
| 4. Migration | Kak Ardell | `internal/migration` |

Urutan: 1 → 2 → (3 dan 4 jalan barengan). Bagian 1 dan 2 dikerjain duluan karena semua fitur numpang login dan cek izin. Folder-nya udah dipisah per orang, jadi kecil kemungkinan bentrok.

Satu aturan yang mahal kalau dilanggar: semua cek izin tulis di `internal/access`. Kalau di handler ketemu `if user.IsAdmin`, itu salah tempat.

### 1. Fazri - Fondasi & Login

- [ ] koneksi db metadata dari `METADATA_DB_URL`
- [ ] migration awal tabel platform: users, groups, group_members, database_connections, group_database_access, sessions, migrations
- [ ] perintah `migrate` dan `seed-admin`
- [ ] router + layout template
- [ ] halaman login, pesan error jangan bedain username vs password
- [ ] hash password (argon2id)
- [ ] session di db (yang disimpan hash-nya), cookie `sid` HttpOnly, SameSite, Secure kalau production
- [ ] idle timeout 30 menit, absolute 8 jam
- [ ] rotasi id saat login / naik privilege / tiap 15 menit, grace 30 detik
- [ ] logout matiin session di db
- [ ] CSRF buat semua request yang ngubah data

### 2. Zahran - Izin & Admin

- [ ] hitung izin efektif user di banyak group (yang tertinggi menang, nggak ada deny)
- [ ] halaman + handler: user, group, anggota group
- [ ] koneksi database target + enkripsi kredensial
- [ ] grant level akses group per database (view_only / view_edit)
- [ ] semua aksi di atas cuma Platform Admin, dan dicek di backend

### 3. Dzakwan - Lihat Database

- [ ] koneksi ke db target pakai kredensial yang didekripsi
- [ ] list schema dan table
- [ ] detail table: column, constraint, index
- [ ] viewer data + pagination
- [ ] koneksi gagal / nggak punya akses -> pesan jelas, bukan 500 kosong

### 4. Kak Ardell - Migration

- [ ] daftar + detail migration (versi, SQL, pelaksana, waktu, status)
- [ ] editor SQL
- [ ] penentuan versi berikutnya, linear per database
- [ ] advisory lock biar eksekusi barengan nggak nabrak
- [ ] jalanin dalam transaction: sukses commit, gagal rollback + simpan error
- [ ] tolak statement yang nggak boleh jalan di transaction (mis. `CREATE INDEX CONCURRENTLY`)
- [ ] cuma view_edit yang boleh bikin dan jalanin migration

### Tes

Tiap orang nge-tes bagiannya sendiri, minimal jalur berhasil + jalur ditolak.

- [ ] unit test per fitur
- [ ] integration test (pakai db khusus tes)
- [ ] `go test ./...` hijau sebelum kumpul

## Log

- 19 Okt (Fazri): inisiasi struktur, postgres 18, pembagian 4 bagian
