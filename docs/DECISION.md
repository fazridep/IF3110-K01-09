# Keputusan Desain

Catatan keputusan yang diambil tim, pakai format ADR singkat. Kalau ada
keputusan yang diganti, jangan dihapus, ubah statusnya jadi "Diganti ADR-xxx".

---

## ADR-001: Package per fitur, izin cuma di `access`

Status: Diterima (19 Okt)

**Konteks.** Spesifikasi minta pengaturan aksi dipisah dari identifikasi user dan
ditaruh di tempat khusus. Kita juga berempat, jadi butuh batas yang jelas biar
nggak sering bentrok.

**Keputusan.**
- Kode dibagi per fitur di `internal/`: `auth`, `access`, `admin`, `dbmgmt`, `migration`.
- `auth` cuma jawab "siapa user ini". Semua keputusan "boleh atau nggak" ada di `access`.
- Query ditaruh di package fiturnya. `store` cuma buka koneksi dan jalanin migration metadata.
- Arah import satu arah: `auth <- access <- admin`, `dbmgmt` dan `migration` pakai `access`.

**Konsekuensi.** Handler nggak boleh ngecek `user.IsPlatformAdmin` sendiri untuk
otorisasi. Field itu boleh dipakai buat tampilan, tapi pengecekannya lewat `access`.

---

## ADR-002: Akses efektif dari banyak group

Status: Diterima (19 Okt)

**Konteks.** User bisa ada di beberapa group yang punya akses ke database yang
sama dengan level berbeda.

**Keputusan.** Level tertinggi yang menang: `view_edit` > `view_only` > tidak
ada akses. Nggak ada aturan deny.

**Konsekuensi.** Cukup `MAX(level)` dari semua grant milik group-group user.
Untuk mencabut akses, user harus dikeluarkan dari semua group yang memberi akses.

---

## ADR-003: Session di server dengan rotasi dan grace period

Status: Diterima (20 Okt)

**Konteks.** Spesifikasi minta session identifier opaque, idle timeout, absolute
expiration, rotasi berkala, grace period, dan session bisa dicabut dari backend.

**Keputusan.**

| Parameter | Nilai | Env |
|-----------|-------|-----|
| Idle timeout | 30 menit | `SESSION_IDLE_TIMEOUT` |
| Absolute expiration | 8 jam sejak login | `SESSION_ABSOLUTE_TIMEOUT` |
| Interval rotasi | 15 menit | `SESSION_ROTATION_INTERVAL` |
| Grace period token lama | 30 detik | `SESSION_GRACE_PERIOD` |

- Token dari `crypto/rand` (128 bit). Yang disimpan di tabel `sessions` cuma
  SHA-256-nya, jadi bocornya isi database nggak langsung bisa dipakai login.
- Cookie `sid`: HttpOnly, SameSite=Lax, Secure kalau `APP_ENV=production`.
  `Expires` cookie selalu sama dengan `expires_at` session.
- Rotasi terjadi saat login (session baru), tiap 15 menit, dan saat privilege
  naik. Untuk yang terakhir, `admin` memanggil `auth.Store.ForceRotate(userID)`
  yang mengosongkan `rotated_at`, jadi request berikutnya dari user itu dirotasi.
- Token lama disimpan di `previous_token_hash` dan masih diterima selama grace
  period. Token lama nggak bisa memicu rotasi lagi.
- Rotasi cuma jalan kalau token yang dikirim masih token aktif
  (`WHERE token_hash = <token lama>`). Kalau ada dua request bareng, hanya satu
  yang merotasi. Request yang kalah tetap dilayani dengan token lama (masih dalam grace).
- Rotasi nggak mengubah `expires_at`.
- Logout mencabut session itu. Login ulang mencabut session lama di browser yang
  sama. Pencabutan akses oleh admin memanggil `RevokeUserSessions(userID)`.

**Konsekuensi.** Tiap request terautentikasi butuh 2-3 query (cari session, ambil
user, update akses). Masih aman untuk skala tugas ini.

---

## ADR-004: CSRF pakai double-submit cookie

Status: Diterima (20 Okt)

**Konteks.** Semua request yang mengubah state butuh perlindungan CSRF, termasuk
form login.

**Keputusan.** Middleware `auth.CSRF` memasang cookie `csrf` (SameSite=Strict,
Secure di production) saat GET. Request selain GET/HEAD/OPTIONS wajib mengirim
token yang sama lewat field `csrf_token` atau header `X-CSRF-Token`. Kalau beda,
dibalas 403.

**Konsekuensi.** Tiap form harus punya
`<input type="hidden" name="csrf_token" value="{{.CSRFToken}}">`. Untuk HTMX,
token dikirim lewat header `X-CSRF-Token`.

---

## ADR-005: Migration metadata pakai runner sendiri

Status: Diterima (20 Okt)

**Konteks.** Tabel platform (users, sessions, groups, dst) perlu dibuat dengan
urutan yang jelas, dan migration-nya ditulis oleh empat orang.

**Keputusan.**
- File `migrations/metadata/NNNN_nama.up.sql` di-embed ke binary dan dijalankan
  `make migrate` urut nama file.
- Versi yang sudah jalan dicatat di tabel `schema_migrations`. Tiap file
  dijalankan dalam satu transaction bersama pencatatannya, jadi kalau gagal,
  semuanya di-rollback.
- Nomor file pakai jatah per orang (lihat `migrations/README.md`).
- File `.down.sql` cuma referensi manual, nggak dijalankan runner.

**Konsekuensi.** File migration yang sudah di-merge ke `dev` jangan diedit lagi.
Perubahan schema ditulis sebagai file baru.

---

## ADR-006: Frontend server-rendered dengan layout bersama

Status: Diterima (20 Okt)

**Konteks.** Spesifikasi menyarankan Go template, dan session berbasis cookie
lebih sederhana tanpa SPA.

**Keputusan.**
- `html/template` dengan file di `web/templates/<fitur>/<halaman>.html`, di-embed ke binary.
- Satu layout di `web/templates/layout/base.html`. Halaman cukup mendefinisikan
  blok `title` dan `content`.
- CSS pakai Pico.css dari CDN. HTMX ditambah kalau ada fitur yang butuh.
- Render lewat `web.Renderer.Render(w, r, "fitur/halaman", data)`. Layout otomatis
  dapat `.User` dan `.CSRFToken`, data halaman ada di `.Data`.
- Package fitur nggak import `web`. Masing-masing mendefinisikan interface
  `Renderer` kecil (contoh di `auth/handler.go`), lalu diisi dari `main`.

**Konsekuensi.** Nggak ada build step frontend. Halaman yang gagal di-parse
langsung ketahuan saat server start.

---

## ADR-007: Integration test pakai schema terpisah per test

Status: Diterima (20 Okt)

**Konteks.** Integration test wajib, dan `go test ./...` menjalankan package
secara paralel ke database yang sama.

**Keputusan.** `testutil.DB(t)` membuat schema acak di `TEST_DATABASE_URL`,
menjalankan migration metadata di schema itu, lalu menghapusnya setelah test
selesai. Kalau `TEST_DATABASE_URL` kosong, test di-skip. `make test` sudah
mengisi env ini ke `db-test`.

**Konsekuensi.** Test nggak saling ganggu dan nggak perlu reset data manual.
Waktu yang dipakai session bisa diatur lewat field `now` di `auth.Store`, jadi
test timeout nggak perlu `sleep`.
