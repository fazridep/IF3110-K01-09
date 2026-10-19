# Progress Tubes IF3110 - Database Management Platform

Terakhir diupdate: **19 Oktober 2026** (rapikan pembagian + struktur)

Deadline Milestone 1: 25 Oktober 2026, 13.10 WIB.

## Pembagian

| Bagian | Orang | Folder |
|--------|-------|--------|
| 1. Identitas & Session | Kak Ardell | `internal/auth`, `internal/web`, `internal/config` |
| 2. Izin & Platform Admin | Dzakwan | `internal/access`, `internal/admin` |
| 3. Database Management | Fazri | `internal/dbmgmt`, `internal/store` |
| 4. Migration | Zahran | `internal/migration`, `internal/testutil` |

Template ikut fitur di `web/templates/<fitur>/`, layout di `web/templates/layout/`.

### Aturan

1. Semua cek izin di `internal/access`. Kalau di handler ketemu
   `if user.IsAdmin`, itu salah tempat.
2. Query ditaruh di package fiturnya masing-masing, bukan di `internal/store`.
3. Arah import jangan dibalik biar nggak import cycle:
   ```
   auth  <-  access  <-  admin
               ^
   dbmgmt -----+
     ^
   migration (pakai access + dbmgmt)
   ```
4. Migration tabel platform di `migrations/metadata/`, nomor sesuai jatah di `migrations/README.md`.

## Hari 1 (19-20 Okt): Fondasi + Kontrak

- [ ] **Semua:** sepakati kontrak (lihat bawah), tulis signature + stub-nya, merge ke main
- [ ] **Fazri:** `store` (pool dari `METADATA_DB_URL`, helper tx) + perintah `migrate`
- [ ] **Kak Ardell:** `config`, router + layout + helper render di `web`
- [ ] **Zahran:** `testutil` (konek `TEST_DATABASE_URL`, jalanin migration, reset data)
- [ ] **Dzakwan:** draft schema tabel users/groups/grant bareng Kak Ardell & Fazri biar FK-nya nyambung

### Kontrak antar bagian

Fungsi yang dipanggil lintas package. Selama implementasi belum jadi, pakai mock dulu.

| Package | Fungsi | Dipakai oleh |
|---------|--------|--------------|
| `auth` | `RequireLogin` (middleware), `CurrentUser(ctx)` | semua handler |
| `auth` | `RevokeUserSessions(ctx, userID)` | admin (saat akses dicabut / user dinonaktifkan) |
| `auth` | `CSRFToken(r)` | template semua fitur |
| `access` | `IsPlatformAdmin(ctx, userID)`, `RequirePlatformAdmin` | admin, dbmgmt |
| `access` | `EffectiveLevel(ctx, userID, dbID)`, `RequireDBAccess(level)` | dbmgmt, migration |
| `dbmgmt` | `Pool(ctx, dbID)` (koneksi ke db target) | migration |

## Detail tugas

### 1. Kak Ardell - Identitas & Session

- [ ] halaman login, pesan error jangan bedain username vs password
- [ ] hash password (argon2id)
- [ ] session di db (yang disimpan hash-nya), cookie `sid` HttpOnly, SameSite, Secure kalau production
- [ ] idle timeout 30 menit, absolute 8 jam
- [ ] rotasi id saat login / naik privilege / tiap 15 menit, grace 30 detik, rotasi nggak memperpanjang absolute
- [ ] logout + `RevokeUserSessions` matiin session di db
- [ ] CSRF buat semua request yang ngubah data
- [ ] password & sid nggak boleh muncul di URL, response, atau log
- [ ] perintah `seed-admin`

### 2. Dzakwan - Izin & Platform Admin

- [ ] hitung izin efektif user di banyak group (yang tertinggi menang, nggak ada deny)
- [ ] `RequirePlatformAdmin` dan `RequireDBAccess(view_only | view_edit)`
- [ ] halaman + handler: user, group, anggota group
- [ ] grant level akses group per database (view_only / view_edit)
- [ ] cabut akses / nonaktifkan user -> panggil `auth.RevokeUserSessions`
- [ ] semua aksi admin dicek di backend, bukan cuma disembunyiin di UI

### 3. Fazri - Database Management

- [ ] CRUD koneksi database target (cuma Platform Admin) + tombol test koneksi
- [ ] enkripsi kredensial (AES-GCM, key dari `CREDENTIAL_ENCRYPTION_KEY`)
- [ ] `Pool(ctx, dbID)`: koneksi ke db target pakai kredensial yang didekripsi
- [ ] list database yang bisa diakses user
- [ ] list schema dan table
- [ ] detail table: column, constraint, index
- [ ] viewer data + pagination
- [ ] koneksi gagal / nggak punya akses -> pesan jelas, bukan 500 kosong

### 4. Zahran - Migration

- [ ] daftar + detail migration (versi, SQL, pelaksana, waktu, status)
- [ ] editor SQL (buat + ubah migration yang belum dijalankan)
- [ ] penentuan versi berikutnya, linear per database
- [ ] advisory lock biar eksekusi barengan nggak nabrak
- [ ] jalanin dalam transaction: sukses commit, gagal rollback + simpan error
- [ ] tolak statement yang nggak boleh jalan di transaction (mis. `CREATE INDEX CONCURRENTLY`)
- [ ] cuma view_edit yang boleh bikin, ubah, dan jalanin; view_only cuma lihat

## Keputusan yang harus didokumentasikan

Nanti dipindah ke `README.md`.

| Hal | Keputusan |
|-----|-----------|
| Akses efektif dari banyak group | level tertinggi menang (`view_edit` > `view_only`), nggak ada deny |
| Idle timeout | 30 menit |
| Absolute expiration | 8 jam sejak login, nggak diperpanjang rotasi |
| Interval rotasi | 15 menit, plus saat login dan naik privilege |
| Grace period id lama | 30 detik |

## Frontend

Go `html/template` + HTMX, CSS pakai Pico.css, editor SQL pakai CodeMirror 6 (atau `<textarea>` dulu).

## Tes

Tiap orang nge-tes bagiannya sendiri, minimal jalur berhasil, gagal, dan ditolak.

- [ ] unit test per fitur
- [ ] integration test pakai `make test-db-up` + `internal/testutil`
- [ ] `go test ./...` hijau sebelum kumpul

## Log

- 19 Okt (Fazri): inisiasi struktur, postgres 18, pembagian 4 bagian
- 19 Okt (Zahran): rapikan struktur, pembagian ulang, kontrak antar bagian, db-test
