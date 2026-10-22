// Package dbmgmt mengurus koneksi ke database target dan membaca isinya.
//
// Isinya:
//   - credential.go: enkripsi password koneksi target pakai AES-GCM
//   - store.go: CRUD database_connections di database metadata
//   - pool.go: pool pgx per koneksi target, plus tes koneksi
//   - introspect.go: daftar schema/tabel, kolom, constraint, index, dan data
//   - handler.go: halaman /admin/databases (Platform Admin) dan /databases
//     (viewer)
//
// Package ini nggak memutuskan siapa yang boleh lihat apa. AdminChecker dan
// LevelResolver diisi dari main, nanti dari internal/access (lihat ADR-009).
package dbmgmt
