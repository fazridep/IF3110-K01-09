// Package dbmgmt mengurus koneksi ke database target dan membaca isinya.
//
// Isinya:
//   - credential.go: enkripsi password koneksi target pakai AES-GCM
//   - store.go: CRUD database_connections di database metadata
//   - pool.go: pool pgx per koneksi target, plus tes koneksi
//   - introspect.go: daftar schema/tabel, kolom, constraint, index, dan data
//   - handler.go: Handler, route, dan helper error bersama
//   - handler_admin.go: halaman /admin/databases (Platform Admin)
//   - handler_viewer.go: halaman /databases (viewer)
//
// Package ini nggak memutuskan siapa yang boleh lihat apa. Aturannya ada di
// internal/access: route admin lewat access.RequirePlatformAdmin, level akses
// per database diambil lewat interface Permissions (ADR-012).
package dbmgmt
