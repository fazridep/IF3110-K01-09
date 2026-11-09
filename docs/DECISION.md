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

Status: diterima, 20 Okt. Poin interface `Renderer` per package diganti ADR-012.

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

Status: diganti ADR-012, 4 Nov.

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

## ADR-012 Helper HTTP bersama dan dbmgmt pakai access langsung

Status: diterima, 4 Nov

Konteks: setelah semua fitur jadi, ada beberapa hal yang ditulis ulang di tiap
package:
- interface `Renderer` ada empat salinan (auth, admin, dbmgmt, migration);
- tiga template `error.html` yang isinya sama, cuma beda tautan kembali;
- tiap handler `fail` nulis `WriteHeader` sebelum render, jadi kalau template
  gagal, client tetap dapat status lama dengan body error;
- cek kode error PostgreSQL (`23505`, `23503`) di-hardcode di dbmgmt dan migration;
- `dbmgmt` masih pakai `AdminChecker`/`LevelResolver` dan konstanta level
  sendiri dari ADR-009, padahal `access` sudah ada. Akibatnya aturan "admin
  atau bukan" ada di dua tempat (`dbmgmt.requireAdmin` dan
  `access.RequirePlatformAdmin`).

Keputusan:
- Package leaf `internal/httpx` (nggak import package internal lain) berisi
  `Renderer`, `Error` + `ErrorPage`, dan `PathUUID`. Semua handler pakai ini.
  `Renderer` punya `RenderStatus`, dan status baru ditulis setelah template
  berhasil dirender.
- Satu halaman error bersama `web/templates/shared/error.html`. Tiap fitur cuma
  ngisi pesan dan tautan kembali.
- Package leaf `internal/pgerr` buat `IsUniqueViolation` dan `IsForeignKeyViolation`.
- `dbmgmt` import `access`: route admin lewat `access.RequirePlatformAdmin`,
  level akses lewat interface kecil `dbmgmt.Permissions`
  (`EffectiveLevel(...) (access.Level, error)`) yang dipenuhi `access.Store`.
- `admin` bergantung ke interface `admin.Sessions`, bukan `*auth.Store`, dan
  validasi level grant pakai `access.Level.Valid`.
- `cmd/server/server.go` satu-satunya tempat package fitur disambung.

Arah import sekarang:

```
httpx, pgerr, config        (leaf, dipakai siapa saja)
auth  <-  access  <-  admin
            ^
          dbmgmt  <-  migration (lewat interface Targets & Permissions)
```

Akibat: nambah fitur baru cukup implement `httpx.Renderer` di test palsu dan
pakai `httpx.Error`. Aturan izin benar-benar cuma di `access` (ADR-001).
Konsekuensinya `dbmgmt` sekarang ikut ke-build ulang kalau `access` berubah,
tapi kontraknya cuma satu method.

## ADR-013 Queue migration di PostgreSQL, lock berupa lease

Status: diterima, 8 Nov

Konteks: spesifikasi M2 minta semua eksekusi lewat queue dan worker. Satu
database cuma boleh menjalankan satu migration, job harus selamat dari restart,
pesan duplikat nggak boleh bikin double commit, dan kalau hasil commit belum
pasti, migration berikutnya ditahan.

Keputusan:
- Queue berupa tabel `migration_jobs`, diambil pakai `FOR UPDATE SKIP LOCKED`.
  Nggak butuh broker baru, dan masuk queue bisa satu transaction dengan data lain.
- Satu approval jadi satu job (`UNIQUE (approval_id)`). Eksekusi dipicu eksplisit
  lewat `POST /api/jobs`, bukan otomatis saat approval terakhir masuk, supaya API
  bisa mengembalikan job ID dan bisa aman dipanggil ulang. `SetOnApproved` di
  package approval nggak dipakai.
- Lock berupa lease di `database_locks`, diambil satu transaction dengan klaim
  job. Worker memperpanjang tiap sepertiga `WORKER_LEASE_TTL`. Tiap klaim punya
  `lease_token`, jadi worker yang lease-nya sudah diambil alih nggak bisa nulis
  hasil lagi, dan eksekusinya dibatalkan.
- FIFO per database: job dilewati kalau ada job lebih lama di database yang sama
  yang masih `queued`, `running`, `uncertain`, atau `partial`. Ini yang menahan
  migration berikutnya.
- Bukti commit ditulis di database target (`_dbplatform.job_commits`), satu
  transaction dengan SQL migration. Primary key `job_id` membuat commit kedua
  gagal, jadi duplikat nggak bisa double commit. Schema ini disembunyikan dari viewer.
- Retry cuma buat kegagalan sementara (`IsTransient`), dibatasi `WORKER_MAX_ATTEMPTS`,
  dengan jeda dua kali lipat plus jitter. Error SQL biasa langsung `failed`.

Pemulihan lock waktu worker mati: lease habis sendiri paling lama `WORKER_LEASE_TTL`
kemudian. Worker lain menandai job-nya `uncertain` dan melepas lock. Sebelum
mengulang, `Executor.Verify` memeriksa bukti commit: kalau ada, job langsung
`success`. Kalau belum, SQL dijalankan ulang. Kalau target nggak bisa dihubungi,
pemeriksaan diulang sampai batas, lalu job tetap `uncertain` dan menahan database
sampai ada yang menekan Periksa ulang (`POST /api/jobs/{id}/recheck`).

Kontrak buat package migration, `queue.Executor`:
- `Execute` menjalankan SQL dan wajib memanggil `queue.RecordCommit` di transaction
  yang sama. Balikan `queue.Partial(err)` kalau sebagian batch sudah committed,
  `queue.Uncertain(err)` kalau COMMIT putus, `queue.Transient(err)` kalau aman diulang.
- `Verify` mengecek bukti, cukup `queue.CommitRecorded`.
- `Finalize` memperbarui data milik migration waktu job selesai, dan harus aman
  dipanggil ulang.
- Progres batch dilaporkan lewat `Reporter.Report`.
- Melanjutkan job partial: `POST /api/jobs` dengan approval berjenis `batch` dan
  `resumeJobId`. Job lama jadi `superseded`, job baru punya `ResumesJobID`.

Akibat: `migration.Executor` sekarang baru mendukung `apply` dalam satu transaction.
`rollback`, `forward`, dan `batch` ditolak sampai model up/down dan riwayat
eksekusi ada. Eksekusi langsung `POST /migrations/{id}/execute` dari M1 juga masih
ada dan melewati approval, harus dihapus waktu halaman migration pindah ke SPA.

Update 9 Nov (ADR-015): `rollback`, `forward`, dan `batch` sudah didukung, dan
halaman migration Go template beserta `POST /migrations/{id}/execute` sudah
dihapus. Job partial sekarang bisa digantikan approval `batch` atau `rollback`.
Kalau `resumeJobId` nggak dikirim, `queue.Service.Submit` mencari job partial
milik migration itu sendiri.

## ADR-014 Struktur folder: fitur dan platform

Status: diterima, 8 Nov

Konteks: `internal/` sudah 15 package sejajar, campuran fitur dan pondasi.
`web/` di root cuma berisi template yang dipakai `internal/web`, dan `PROGRESS.md`
terpisah dari dokumen lain.

Keputusan:
- Pondasi tanpa logika fitur pindah ke `internal/platform/`: `config`, `httpx`,
  `obs`, `pgerr`, `store`, `testutil`. Aturannya, package di situ nggak boleh
  import package fitur.
- Package fitur tetap di `internal/<fitur>`.
- `internal/web` tetap di luar `platform`, karena menyusun route dan bergantung ke
  `auth`.
- Template pindah ke `internal/web/templates`, folder `web/` di root dihapus.
- `PROGRESS.md` pindah ke `docs/`, dan `frontend/src/auth` jadi `src/features/auth`.

Akibat: semua import `internal/{config,httpx,obs,pgerr,store,testutil}` berubah.
Branch yang masih jalan cukup mengganti path import itu setelah rebase. Path lama
di ADR sebelumnya sengaja dibiarkan sebagai catatan waktu keputusan dibuat.

## ADR-015 Migration up/down, riwayat eksekusi, dan mode batch

Status: diterima, 9 Nov

Konteks: spesifikasi M2 minta tiap migration punya SQL up dan down (atau
irreversible beserta alasannya), rollback cuma dari migration aktif terakhir,
forward urut, semua eksekusi tercatat, history cuma berisi yang committed dan
nggak boleh ditimpa, plus mode batch dengan checkpoint.

Keputusan:
- `database_migrations` dapat kolom `down_sql`, `irreversible`,
  `irreversible_reason`, `mode`, dan konfigurasi batch. Isinya cuma bisa diubah
  selama `draft`. Mengubah draft membatalkan approval yang belum jadi job,
  dan ditolak kalau sudah ada job yang antre atau jalan.
- Status migration jadi posisinya di database: `draft`, `running` (apply
  pertama), `success` (aktif), `failed`, `rolled_back`, `partial`, dan
  `partially_reverted`. Rollback atau forward yang gagal nggak mengubah status.
- Apply yang gagal jadi `failed` cuma kalau SQL-nya sempat dikirim ke database
  target (`migration_executions.sql_started`). Kalau gagal sebelum itu, misalnya
  target nggak bisa dihubungi sampai percobaan habis atau tabel batch hilang,
  migration balik ke `draft` beserta pesan error-nya. SQL yang belum pernah
  dieksekusi belum immutable, jadi cukup diajukan approval lagi tanpa bikin
  migration baru. Eksekusi yang gagal tetap tercatat di riwayat.
- Satu eksekusi (apply, forward, rollback) satu baris di `migration_executions`.
  ID eksekusi beda dengan ID job, karena eksekusi batch bisa dikerjakan beberapa
  job (retry atau dilanjutkan). Rollback menyimpan `reverts_execution_id`.
- `migration_history` cuma diisi waktu eksekusi selesai dan committed. Isinya
  salinan SQL yang dijalankan, versi, pelaksana, dan waktu. Trigger menolak
  UPDATE dan DELETE.
- Aturan urutan (`actionRule`): rollback cuma buat migration dengan versi
  tertinggi yang masih berlaku dan nggak irreversible. Forward cuma buat
  migration `rolled_back` terendah di atas yang masih berlaku. Apply ditahan
  kalau ada migration lain yang `partial` atau `partially_reverted`.
- Aturan itu dicek dua kali: waktu approval diajukan (hook
  `approval.SetActionCheck`, balas 409 `action-not-allowed`) dan waktu worker
  mengambil job (revalidasi).
- Hide rollback memasangkan lewat execution ID: rollback sukses dan eksekusi
  yang dibatalkannya disembunyikan. Rollback gagal dan eksekusi gagal tetap
  terlihat. Filternya cuma di tampilan, data nggak dihapus.

Mode batch:
- User mengisi tabel, kolom kursor (wajib unik, dicek ke database target), dan
  ukuran batch. Tiap batch, worker mengisi temp table `batch_rows` dengan row
  berikutnya (`WHERE key > kursor ORDER BY key LIMIT ukuran`), lalu menjalankan
  SQL up. SQL up dan down wajib memakai `batch_rows`.
- Cakupan ditentukan sekali waktu eksekusi mulai: kunci terbesar saat itu
  disimpan di `_dbplatform.batch_scope`. Row yang masuk belakangan nggak ikut.
- Checkpoint (`_dbplatform.batch_checkpoints`: eksekusi, nomor batch, rentang
  kursor, jumlah row) ditulis di database target, satu transaction dengan
  perubahan batch-nya. Primary key (eksekusi, batch) mencegah batch yang sama
  ke-commit dua kali. Metadata cuma menyimpan cerminnya buat tampilan.
- Kalau opsi data pemulihan dicentang, isi row sebelum diubah disimpan sebagai
  JSONB di `_dbplatform.batch_backup`. Waktu rollback, `batch_rows` diisi dari
  situ, jadi SQL down bisa mengembalikan nilai persis.
- Batch gagal: cuma transaction batch itu yang batal. Kalau sudah ada batch
  committed, hasilnya `queue.Partial` dan migration jadi `partial`. Kegagalan
  sementara diulang worker dan lanjut dari checkpoint.
- Lanjutkan (`batch`) meneruskan eksekusi yang sama dari checkpoint. Rollback
  membatalkan batch yang sudah committed urut dari batch terakhir. Kalau
  berhenti di tengah, jadi `partially_reverted` dan rollback berikutnya
  melanjutkan dari batch yang belum dibatalkan.
- Bukti commit job (`queue.RecordCommit`) ditulis di transaction terakhir
  setelah semua batch selesai. Job yang terputus di tengah diulang, dan batch
  yang sudah ada checkpoint-nya dilewati.

Akibat: database target dapat schema `_dbplatform` berisi tiga tabel tambahan.
Data pemulihan bikin batch lebih lambat dan makan tempat, jadi opsional.
Migration M1 yang sudah jalan ditandai irreversible karena nggak punya SQL down.

