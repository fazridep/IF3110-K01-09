# Migrations

Migration untuk database metadata platform. Jalankan dengan `make migrate`.
Versi yang sudah jalan dicatat di tabel `schema_migrations`, jadi aman
dijalankan berulang.

Migration database target yang dibuat user lewat UI ada di `internal/migration`,
bukan di sini.

## Penomoran

| Nomor | Pemilik | Tabel |
|-------|---------|-------|
| 0001-0099 | bersama | - |
| 0100-0199 | auth | users, sessions |
| 0200-0299 | dbmgmt | database_connections |
| 0300-0399 | access, admin | groups, group_members, group_database_access |
| 0400-0499 | migration | database_migrations |

Urutannya ngikutin foreign key, jadi tabel yang dirujuk dibuat lebih dulu.

Nama file `NNNN_deskripsi.up.sql`. File `.down.sql` boleh ada, tapi nggak
dijalankan otomatis.
