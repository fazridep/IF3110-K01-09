# Migrations

`metadata/` berisi migration schema database **platform** (users, groups,
sessions, dst), dijalankan lewat `make migrate`. Versi yang sudah jalan dicatat
di tabel `schema_migrations`, jadi `make migrate` aman diulang.

Ini beda dengan fitur migration untuk database **target** di
`internal/migration` (yang dibuat user lewat UI).

## Penamaan

Tiap orang bikin migration untuk tabel miliknya sendiri, nomornya pakai jatah
masing-masing biar nggak tabrakan:

| Nomor | Pemilik | Tabel |
|-------|---------|-------|
| `0001`–`0099` | Fondasi | tabel bersama kalau ada |
| `0100`–`0199` | auth | users, sessions |
| `0200`–`0299` | dbmgmt | database_connections |
| `0300`–`0399` | access/admin | groups, group_members, group_database_access |
| `0400`–`0499` | migration | migrations (riwayat migration db target) |

Urutan ini sengaja ngikutin foreign key (grant butuh users dan
database_connections udah ada duluan).

Format: `NNNN_deskripsi.up.sql` (dan `.down.sql` kalau perlu, tapi cuma buat
referensi manual, nggak dijalankan runner).

File yang sudah di-merge ke `dev` jangan diedit lagi, bikin file baru.
