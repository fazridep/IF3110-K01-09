# Progress Tubes IF3110 - Database Management Platform

Terakhir diupdate: **22 Oktober 2026** (access + admin selesai)

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
- [x] **Fazri:** `store` (pool dari `METADATA_DB_URL`) + perintah `migrate`
- [ ] **Fazri:** helper transaction di `store`
- [x] **Kak Ardell:** `config`, router + layout + helper render di `web`
- [x] **Zahran:** `testutil` (konek `TEST_DATABASE_URL`, jalanin migration, schema per test)
- [x] **Dzakwan:** draft schema tabel users/groups/grant bareng Kak Ardell & Fazri biar FK-nya nyambung

### Kontrak antar bagian

Fungsi yang dipanggil lintas package. Selama implementasi belum jadi, pakai mock dulu.
ID user bertipe `uuid.UUID`, jadi foreign key ke `users(id)` pakai `UUID`.

| Package | Fungsi | Dipakai oleh |
|---------|--------|--------------|
| `auth` | `(*Store).RequireLogin` (middleware), `CurrentUser(ctx)` | semua handler |
| `auth` | `(*Store).RevokeUserSessions(ctx, userID)` | admin (saat akses dicabut / user dinonaktifkan) |
| `auth` | `(*Store).ForceRotate(ctx, userID)` | admin (saat user dijadikan Platform Admin) |
| `web` | `(*Renderer).Render(w, r, "fitur/halaman", data)` | semua handler yang nampilin halaman |
| `access` | `IsPlatformAdmin(ctx, userID)`, `RequirePlatformAdmin` | admin, dbmgmt |
| `access` | `EffectiveLevel(ctx, userID, dbID)`, `RequireDBAccess(level)` | dbmgmt, migration |
| `access` | `CanView(ctx, userID, dbID)`, `CanEdit(ctx, userID, dbID)` balikin bool | migration |
| `dbmgmt` | `Pool(ctx, dbID)` (koneksi ke db target) | migration |

## Detail tugas

### 1. Kak Ardell - Identitas & Session

- [x] halaman login, pesan error jangan bedain username vs password
- [x] hash password (argon2id)
- [x] session di db (yang disimpan hash-nya), cookie `sid` HttpOnly, SameSite, Secure kalau production
- [x] idle timeout 30 menit, absolute 8 jam
- [x] rotasi id saat login / naik privilege / tiap 15 menit, grace 30 detik, rotasi nggak memperpanjang absolute
- [x] logout + `RevokeUserSessions` matiin session di db
- [x] CSRF buat semua request yang ngubah data
- [x] password & sid nggak boleh muncul di URL, response, atau log
- [x] perintah `seed-admin`
- [x] unit + integration test session, login, logout, CSRF

### 2. Dzakwan - Izin & Platform Admin

- [x] hitung izin efektif user di banyak group (yang tertinggi menang, nggak ada deny)
- [x] `RequirePlatformAdmin` dan `RequireDBAccess(view_only | view_edit)`
- [x] halaman + handler: user, group, anggota group
- [x] grant level akses group per database (view_only / view_edit)
- [x] tambah user biasa dari halaman admin
- [x] cabut akses -> panggil `auth.RevokeUserSessions`
- [ ] nonaktifkan user
- [x] semua aksi admin dicek di backend, bukan cuma disembunyiin di UI

### 3. Fazri - Database Management

- [x] CRUD koneksi database target (cuma Platform Admin) + tombol test koneksi
- [x] enkripsi kredensial (AES-GCM, key dari `CREDENTIAL_ENCRYPTION_KEY`)
- [x] `Pool(ctx, dbID)`: koneksi ke db target pakai kredensial yang didekripsi
- [x] list database yang bisa diakses user
- [x] list schema dan table
- [x] detail table: column, constraint, index
- [x] viewer data + pagination
- [x] koneksi gagal / nggak punya akses -> pesan jelas, bukan 500 kosong

### 4. Zahran - Migration

- [x] daftar + detail migration (versi, SQL, pelaksana, waktu, status)
- [x] editor SQL (buat + ubah migration yang belum dijalankan)
- [x] penentuan versi berikutnya, linear per database
- [x] advisory lock biar eksekusi barengan nggak nabrak
- [x] jalanin dalam transaction: sukses commit, gagal rollback + simpan error
- [x] tolak statement yang nggak boleh jalan di transaction (mis. `CREATE INDEX CONCURRENTLY`)
- [x] cuma view_edit yang boleh bikin, ubah, dan jalanin; view_only cuma lihat
- [ ] daftarin route migration di `main.go`, `CanView`/`CanEdit` dari `access.EffectiveLevel` (integrasi 24 Okt)

## Keputusan desain

Semua ada di [`docs/DECISION.md`](docs/DECISION.md): struktur, akses efektif,
session, CSRF, migration metadata, frontend, dan cara integration test.
Kalau bikin keputusan baru, tambahin ADR di sana.

## Tes

Tiap orang nge-tes bagiannya sendiri, minimal jalur berhasil, gagal, dan ditolak.

- [ ] unit test per fitur
- [ ] integration test pakai `make test-db-up` + `testutil.DB(t)`
- [ ] `go test ./...` hijau sebelum kumpul

## Log

- 19 Okt (Fazri): inisiasi struktur, postgres 18, pembagian 4 bagian
- 19 Okt (Zahran): rapikan struktur, pembagian ulang, kontrak antar bagian, db-test
- 20 Okt (Kak Ardell): login, session, CSRF, seed-admin, config, router
- 20 Okt (Zahran): review auth, fix rotasi session yang bisa bikin ke-logout, layout template,
  runner `migrate`, `testutil`, test auth, `docs/DECISION.md`
- 22 Okt (Fazri): migration `0200_database_connections`, CRUD koneksi + enkripsi AES-GCM,
  pool ke db target, viewer schema/tabel/data dengan pagination, unit + integration test,
  route `/admin/databases` dan `/databases`
- 22 Okt (Dzakwan): migration 0300-0302 (groups, group_members, group_database_access),
  package `access` (EffectiveLevel, RequirePlatformAdmin, RequireDBAccess),
  package `admin` (CRUD user/group/member/grant, halaman admin), test access + admin
- 22 Okt (Zahran): migration `0400_database_migrations`, package `migration` (cek SQL, service,
  handler, template), unit + integration test, ADR-010
- 22 Okt (Zahran): review dbmgmt, fix tampilan nilai di viewer, lock pool, dan tes koneksi di form edit
- 22 Okt (Zahran): review access + admin, rebase ke dev, sambung access ke dbmgmt di `main.go`,
  FK grant ke `database_connections`, form tambah user, admin nggak otomatis punya akses database
