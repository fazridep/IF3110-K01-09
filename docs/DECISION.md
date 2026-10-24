# Keputusan Desain

Catatan keputusan tim. Kalau ada yang diganti, jangan dihapus, cukup ubah
statusnya jadi "diganti ADR-xxx".

## ADR-001 Package per fitur, izin cuma di access

Status: diterima, 19 Okt

Konteks: spesifikasi minta aturan aksi dipisah dari identifikasi user. Kita
berempat, jadi perlu batas yang jelas biar nggak sering bentrok.

Keputusan:
- Kode dibagi per fitur di `internal/`: `auth`, `access`, `admin`, `dbmgmt`, `migration`.
- `auth` cuma ngurus siapa user-nya. Boleh atau nggaknya suatu aksi diputuskan di `access`.
- Query ditaruh di package fiturnya. `store` cuma buka koneksi dan jalanin migration metadata.
- Arah import: `auth <- access <- admin`, `dbmgmt` dan `migration` pakai `access`.

Akibat: handler nggak boleh ngecek `user.IsPlatformAdmin` sendiri buat
otorisasi. Field itu cuma buat tampilan.

## ADR-002 Akses efektif dari banyak group

Status: diterima, 19 Okt

Konteks: satu user bisa masuk beberapa group yang punya akses ke database yang
sama dengan level beda.

Keputusan: level tertinggi yang dipakai, `view_edit` > `view_only` > nggak ada
akses. Nggak ada aturan deny.

Akibat: cukup ambil `MAX(level)` dari semua grant group user. Buat nyabut akses,
user harus dikeluarkan dari semua group yang ngasih akses itu.

## ADR-003 Session di server, dengan rotasi dan grace period

Status: diterima, 20 Okt

Konteks: spesifikasi minta session id yang opaque, idle timeout, absolute
expiration, rotasi berkala, grace period, dan session bisa dicabut dari backend.

Keputusan:

| Parameter | Nilai | Env |
|-----------|-------|-----|
| Idle timeout | 30 menit | `SESSION_IDLE_TIMEOUT` |
| Absolute expiration | 8 jam sejak login | `SESSION_ABSOLUTE_TIMEOUT` |
| Interval rotasi | 15 menit | `SESSION_ROTATION_INTERVAL` |
| Grace period token lama | 30 detik | `SESSION_GRACE_PERIOD` |

- Token 128 bit dari `crypto/rand`. Di tabel `sessions` yang disimpan cuma hash SHA-256-nya.
- Cookie `sid` pakai HttpOnly dan SameSite=Lax, plus Secure kalau `APP_ENV=production`.
  `Expires` cookie sama dengan `expires_at` session.
- Rotasi waktu login, tiap 15 menit, dan waktu privilege naik. Yang terakhir lewat
  `auth.Store.ForceRotate(userID)`, yang ngosongin `rotated_at` supaya request
  berikutnya dirotasi.
- Token lama disimpan di `previous_token_hash` dan masih diterima selama grace
  period, tapi nggak bisa memicu rotasi lagi.
- Rotasi cuma jalan kalau token yang dikirim masih token aktif. Kalau dua request
  datang bareng, cuma satu yang merotasi, yang satunya tetap jalan pakai token lama.
- Rotasi nggak mengubah `expires_at`.
- Logout mencabut session itu, login ulang mencabut session lama di browser yang
  sama, dan admin yang mencabut akses memanggil `RevokeUserSessions(userID)`.

Akibat: tiap request yang butuh login jalan 2-3 query (cari session, ambil user,
update waktu akses). Buat skala tugas ini masih aman.

## ADR-004 CSRF pakai double-submit cookie

Status: diterima, 20 Okt

Konteks: semua request yang ngubah data butuh perlindungan CSRF, termasuk form login.

Keputusan: middleware `auth.CSRF` masang cookie `csrf` waktu GET (SameSite=Strict,
Secure di production). Request selain GET/HEAD/OPTIONS wajib ngirim token yang
sama lewat field `csrf_token` atau header `X-CSRF-Token`. Kalau beda, dibalas 403.

Akibat: tiap form wajib punya
`<input type="hidden" name="csrf_token" value="{{.CSRFToken}}">`. Kalau pakai
HTMX, token dikirim lewat header `X-CSRF-Token`.

## ADR-005 Migration metadata pakai runner sendiri

Status: diterima, 20 Okt

Konteks: tabel platform (users, sessions, groups, dst) harus dibuat urut, dan
file migration-nya ditulis empat orang.

Keputusan:
- File `migrations/metadata/NNNN_nama.up.sql` di-embed ke binary, lalu
  `make migrate` jalanin urut nama file.
- Versi yang sudah jalan dicatat di `schema_migrations`. Satu file dan
  pencatatannya ada di satu transaction, jadi kalau gagal semuanya di-rollback.
- Nomor file pakai jatah per orang, lihat `migrations/README.md`.
- `.down.sql` cuma buat referensi, nggak dijalankan runner.

Akibat: file migration yang sudah masuk `dev` jangan diedit lagi. Kalau schema
berubah, bikin file baru.

## ADR-006 Frontend server-rendered dengan layout bersama

Status: diterima, 20 Okt

Konteks: spesifikasi menyarankan Go template, dan session berbasis cookie lebih
gampang tanpa SPA.

Keputusan:
- Pakai `html/template`, file di `web/templates/<fitur>/<halaman>.html` dan di-embed ke binary.
- Layout cuma satu, `web/templates/layout/base.html`. Halaman tinggal isi blok
  `title` dan `content`.
- CSS pakai Pico.css dari CDN. HTMX ditambah kalau memang perlu.
- Render pakai `web.Renderer.Render(w, r, "fitur/halaman", data)`. Layout dapat
  `.User` dan `.CSRFToken`, data halaman ada di `.Data`.
- Package fitur nggak import `web`. Tiap package bikin interface `Renderer`
  sendiri (contohnya di `auth/handler.go`), terus diisi dari `main`.

Akibat: nggak ada build step frontend, dan template yang salah langsung ketahuan
waktu server start.

## ADR-007 Integration test pakai schema terpisah per test

Status: diterima, 20 Okt

Konteks: integration test wajib, dan `go test ./...` menjalankan package secara
paralel ke database yang sama.

Keputusan: `testutil.DB(t)` bikin schema acak di `TEST_DATABASE_URL`, jalanin
migration metadata di situ, lalu hapus schema-nya setelah test selesai. Kalau
`TEST_DATABASE_URL` kosong, test di-skip. `make test` sudah ngisi env ini ke `db-test`.

Akibat: test nggak saling ganggu dan nggak perlu reset data manual. Waktu di
`auth.Store` bisa diatur lewat field `now`, jadi test timeout nggak perlu `sleep`.

## ADR-008 Kredensial koneksi target pakai AES-GCM

Status: diterima, 22 Okt

Konteks: Platform Admin menyimpan host, port, username, dan password database
target di database metadata. Password itu nggak boleh kebaca cuma gara-gara
dump metadata bocor, dan nggak boleh ikut ke log.

Keputusan:
- Password dienkripsi AES-256-GCM sebelum masuk kolom `password_enc`.
- Kunci diambil dari `CREDENTIAL_ENCRYPTION_KEY` (base64, 32 byte) di env,
  bukan disimpan di database.
- Nonce acak tiap enkripsi, jadi password yang sama hasil ciphertext-nya beda.
- Server menolak start kalau kunci kosong atau panjangnya salah. `migrate` dan
  `seed-admin` tetap jalan tanpa kunci.
- Pool ke database target di-cache per koneksi, dan dibuang kalau koneksi
  diubah atau dihapus supaya credential baru langsung kepakai.
- Pesan error koneksi cuma menyebut host, port, dan nama database. Password
  nggak pernah masuk error atau log.

Akibat: backup metadata harus disimpan bareng kuncinya, di tempat terpisah.
Kalau kunci diganti, kredensial lama nggak bisa dibuka dan admin harus input
ulang password-nya.

## ADR-009 dbmgmt menunggu access lewat fungsi yang di-inject

Status: selesai, 22 Okt. Dua fungsi di `main.go` sudah disambung ke `access`.

Konteks: `dbmgmt` butuh dua hal dari `access`: apakah user Platform Admin, dan
level aksesnya ke satu database. Package `access` belum ada waktu dbmgmt
dikerjakan, jadi import langsung bakal bikin build pecah.

Keputusan: `dbmgmt.NewHandler` menerima `AdminChecker` dan `LevelResolver`
(fungsi biasa), bukan import `access`. Di `main.go` sementara diisi cek
`IsPlatformAdmin` dari session dan resolver yang selalu "tanpa akses".

Akibat: `/admin/databases` sudah bisa dipakai, tapi `/databases` masih kosong
sampai `access` di-merge. Penggantiannya cuma dua fungsi di `runServer`, handler
nggak perlu diubah.

## ADR-010 Siklus hidup migration database target

Status: diusulkan, 20 Okt

Konteks: spesifikasi minta riwayat migration linear per database, lengkap dengan
versi, SQL, pelaksana, waktu, dan status. Eksekusinya harus dalam transaction,
dan cuma user `view_edit` yang boleh bikin, ubah, dan jalanin.

Keputusan:
- Disimpan di tabel `database_migrations`. Status `draft -> running -> success | failed`.
- Versi dikasih waktu draft dibuat, yaitu versi terakhir di database itu + 1.
  Per database cuma boleh ada satu migration `draft` atau `running` (unique index
  parsial), jadi riwayatnya nggak bercabang.
- Draft masih boleh diedit. Setelah dijalankan, record-nya nggak bisa diubah lagi.
  Yang gagal tetap tercatat `failed` beserta error-nya, perbaikannya dibuat
  sebagai migration baru.
- Waktu execute, status diubah ke `running` dengan syarat masih `draft`, jadi
  klik dua kali cuma jalan sekali. SQL-nya dijalankan di database target dalam
  satu transaction yang pegang `pg_advisory_xact_lock`. Sukses di-commit, gagal
  di-rollback, lalu hasilnya disimpan.
- SQL dicek dulu sebelum disimpan dan sebelum dijalankan (`CheckSQL`). Yang
  ditolak: `CONCURRENTLY` (kecuali `REFRESH MATERIALIZED VIEW`), `VACUUM`,
  `CREATE/DROP DATABASE`, `CREATE/DROP TABLESPACE`, `ALTER SYSTEM`, dan
  `BEGIN`/`COMMIT`/`ROLLBACK`.
- FK `database_id` ke `database_connections` sengaja tanpa CASCADE. Koneksi yang
  sudah punya riwayat migration nggak bisa dihapus, biar riwayatnya nggak hilang.
- Package `migration` nggak import `access` dan `dbmgmt`. Yang dibutuhkan ditulis
  sebagai interface `Permissions` (`CanView`, `CanEdit`) dan `Targets` (`Pool`),
  terus disambungkan di `main.go`.

Akibat: riwayat bisa berisi migration `failed`, dan itu disengaja biar percobaan
yang gagal tetap kelihatan. Kalau server mati di tengah eksekusi, transaction di
database target otomatis rollback, tapi record-nya bisa nyangkut di `running`
dan harus dibenerin manual.

## ADR-011 Platform Admin bukan level akses database

Status: diterima, 22 Okt

Konteks: spesifikasi bilang Platform Admin mengelola user, group, koneksi, dan
grant, tapi "bukan tingkatan akses terhadap suatu database".

Keputusan: `access.EffectiveLevel` cuma ngitung dari grant group. Admin yang
mau lihat isi database atau bikin migration harus masuk group yang punya akses,
sama kayak user lain.

Akibat: admin bisa ngatur `/admin/*` tanpa otomatis bisa baca data. Buat demo,
admin perlu dimasukin ke group dulu.
