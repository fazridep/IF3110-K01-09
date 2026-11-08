# Konvensi API

Semua endpoint JSON ada di bawah `/api/`. Frontend SPA cuma bicara ke backend
lewat sini. Konvensinya mengikuti materi REST API Design (06). Helper Go-nya
ada di `internal/platform/httpx/json.go`, client-nya di `frontend/src/lib/api.ts`.

## Ringkasan

| Hal | Aturan |
|-----|--------|
| Path | kata benda jamak, kebab/lower case, id stabil: `/api/databases/{id}/migrations` |
| Body | JSON, field **camelCase** |
| Waktu | RFC 3339 UTC, contoh `"2026-11-06T09:00:00Z"`. `time.Time` otomatis begitu |
| Koleksi | selalu `{"items": [...]}`. Kosong tetap `{"items": []}`, bukan `null` |
| Paging | `?limit=` + `?cursor=`, response `nextCursor` kalau masih ada |
| Error | `application/problem+json` (RFC 9457), lihat bawah |
| Cache | response API `Cache-Control: no-store` (data per user) |
| Login | cookie `sid` HttpOnly, dikirim otomatis browser |
| CSRF | request selain GET/HEAD wajib header `X-CSRF-Token` = isi cookie `csrf` |
| Korelasi | tiap response bawa `X-Request-ID` dan `X-Trace-ID` |

## Status code

| Kode | Dipakai untuk | Helper Go |
|------|---------------|-----------|
| 200 | baca atau ubah, ada body | `httpx.JSON(w, 200, v)` |
| 201 | resource baru + header `Location` | `httpx.Created(w, "/api/groups/"+id, v)` |
| 202 | job masuk queue (migration, rollback, forward). **Bukan** tanda sukses | `httpx.Accepted(w, httpx.JobAccepted{...})` |
| 204 | hapus/logout, tanpa body | `httpx.NoContent(w)` |
| 400 | input salah, plus daftar field | `httpx.Invalid(w, r, detail, fields...)` |
| 401 | belum login / session habis | otomatis dari `RequireAPILogin` |
| 403 | sudah login tapi nggak boleh | `httpx.Fail(w, r, 403, httpx.CodeForbidden, detail)` |
| 404 | nggak ada, **atau ada tapi user nggak punya akses** (cegah BOLA) | `httpx.NotFound(w, r, detail)` |
| 405 | method salah, plus header `Allow` | otomatis dari router |
| 409 | bentrok dengan keadaan sekarang (migration lain jalan, dsb) | `httpx.Fail(w, r, 409, "kode", detail)` |
| 415 | body bukan JSON | otomatis dari `httpx.Decode` |
| 500 | bug/server error. Detail cuma di log | `httpx.ServerError(w, r, err)` |
| 502 | database target nggak bisa dihubungi | `httpx.Fail(w, r, 502, httpx.CodeUnavailable, detail)` |

## Format error

```http
HTTP/1.1 400 Bad Request
Content-Type: application/problem+json

{
  "type": "urn:problem:invalid-input",
  "title": "Input tidak valid",
  "status": 400,
  "detail": "Username dan password wajib diisi.",
  "instance": "/api/session",
  "errors": [
    { "field": "username", "detail": "wajib diisi" },
    { "field": "password", "detail": "wajib diisi" }
  ],
  "requestId": "vxz3rvgk2nbqn4viwfhuzujavm"
}
```

- `type` URN stabil `urn:problem:<kode>`. Client boleh cek ini (lewat
  `ApiError.code` di SPA). Kode umum ada di konstanta `httpx.Code*`. Fitur
  boleh nambah kode sendiri, misal `migration-not-reversible`,
  `approval-required`, `database-locked`.
- `detail` pesan buat user (bahasa Indonesia). Jangan isi stack trace, SQL
  internal, atau credential.
- `errors` cuma untuk validasi per field, `field` pakai nama JSON.
- `requestId` dipakai buat nyari log di server.

## Menulis handler JSON

```go
// RegisterAPIRoutes dipanggil dari cmd/server/server.go (Options.RegisterAPI).
// Semua route di sini sudah lewat RequireAPILogin: auth.CurrentUser pasti ada.
func (h *Handler) RegisterAPIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/groups", h.apiListGroups)
	mux.HandleFunc("POST /api/groups", h.apiCreateGroup)
}

type groupJSON struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

func (h *Handler) apiCreateGroup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		httpx.Invalid(w, r, "Nama group wajib diisi.", httpx.FieldError{Field: "name", Detail: "wajib diisi"})
		return
	}
	g, err := h.store.CreateGroup(r.Context(), in.Name)
	if err != nil {
		httpx.ServerError(w, r, err)
		return
	}
	httpx.Created(w, "/api/groups/"+g.ID.String(), groupJSON{g.ID, g.Name, g.CreatedAt})
}
```

Catatan:

- Bikin struct `...JSON` khusus response, jangan kirim struct domain mentah.
  Kirim cuma field yang dibutuhkan tampilan.
- Cek izin per object di setiap endpoint (lewat `access`). Kalau nggak boleh
  lihat, balas 404 supaya keberadaan resource nggak bocor.
- Untuk job async, simpan job dulu, balas 202, worker yang jalanin:

  ```go
  httpx.Accepted(w, httpx.JobAccepted{JobID: id, StatusURL: "/api/jobs/" + id})
  // {"jobId":"...","status":"queued","statusUrl":"/api/jobs/..."}
  ```

  SPA lalu polling `statusUrl` (lihat docs/frontend.md).

## Endpoint auth (bagian 1)

| Method | Path | Login | Body | Response |
|--------|------|-------|------|----------|
| `POST` | `/api/session` | tidak | `{"username","password"}` | `201` user + Set-Cookie `sid`. `400` field kosong, `401` `invalid-credentials`, `403` `account-disabled` |
| `DELETE` | `/api/session` | tidak | - | `204`, cookie dihapus, session dicabut. Aman diulang |
| `GET` | `/api/me` | ya | - | `200` user, `401` kalau belum login |
| `GET` | `/api/health` | tidak | - | `200 {"status":"ok"}` |

User:

```json
{ "id": "161be72d-7d87-4a8c-ae08-259bc4a76dfd", "username": "admin", "isPlatformAdmin": true }
```

`isPlatformAdmin` cuma buat nampilin menu admin di SPA. Izin tetap dicek
server (ADR-001).

Pesan `401` untuk username tidak ada dan password salah sengaja sama.
Session tetap mengikuti ADR-003 (idle 30 menit, absolute 8 jam, rotasi 15
menit + grace 30 detik). Token hasil rotasi dikirim lewat `Set-Cookie` di
response API mana pun, browser yang menyimpannya.

## Endpoint database (bagian 3)

Semua perlu login. Yang nggak punya akses ke database itu dapat `404`.

| Method | Path | Response |
|--------|------|----------|
| `GET` | `/api/databases` | `{"items":[{id,name,database,level}]}`, cuma yang boleh dibuka user |
| `GET` | `/api/databases/{id}` | database + `schemas[].tables[]`. `502` kalau target mati |
| `GET` | `/api/databases/{id}/tables/{schema}/{table}?page=&perPage=` | `columns`, `constraints`, `indexes`, `rows`. Sel NULL jadi `null` |

Khusus Platform Admin (lainnya `403`). Password nggak pernah dikirim balik.

| Method | Path | Body | Response |
|--------|------|------|----------|
| `GET` | `/api/admin/connections` | - | daftar koneksi |
| `POST` | `/api/admin/connections` | `{name,host,port,database,username,password,sslmode}` | `201`. `400` per field, `409` nama kembar |
| `GET` | `/api/admin/connections/{id}` | - | koneksi |
| `PUT` | `/api/admin/connections/{id}` | sama, password kosong berarti tetap | koneksi |
| `DELETE` | `/api/admin/connections/{id}` | - | `204`. `409` `connection-in-use` kalau punya riwayat migration |
| `POST` | `/api/admin/connections/test` | sama, plus `id` opsional buat memakai password tersimpan | `200 {ok,message}` |
| `POST` | `/api/admin/connections/{id}/test` | - | `200 {ok,message}` |

## Endpoint job (bagian 3)

| Method | Path | Body | Response |
|--------|------|------|----------|
| `POST` | `/api/jobs` | `{approvalId, resumeJobId?}` | `202 {jobId,status,statusUrl}`. Kirim ulang approval yang sama dapat job yang sama. `403` kalau bukan View + Edit, `404` kalau nggak boleh lihat, `409` `approval-required` / `invalid-resume` |
| `GET` | `/api/jobs?databaseId=&status=&limit=&cursor=` | - | job di database yang boleh dilihat, terbaru dulu, plus `nextCursor` |
| `GET` | `/api/jobs/{id}` | - | job. `404` kalau nggak boleh lihat |
| `POST` | `/api/jobs/{id}/recheck` | - | periksa ulang job `uncertain`. `409` kalau statusnya lain |

Status job: `queued`, `running`, `success`, `failed`, `partial`, `uncertain`,
`superseded`. `202` cuma berarti masuk queue, hasilnya dipantau lewat `statusUrl`.

## Contoh pakai curl

```bash
curl -c cj -b cj -s localhost:8080/api/health >/dev/null
CSRF=$(awk '$6=="csrf"{print $7}' cj)
curl -c cj -b cj -H "X-CSRF-Token: $CSRF" -H 'Content-Type: application/json' \
     -d '{"username":"admin","password":"..."}' localhost:8080/api/session
curl -b cj localhost:8080/api/me
```

## Nama endpoint per fitur (usulan)

Pemilik fitur yang memutuskan bentuk akhirnya. Yang penting prefix-nya nggak
tabrakan:

| Fitur | Pemilik | Prefix |
|-------|---------|--------|
| Database viewer | Fazri | `/api/databases`, `/api/databases/{id}/tables/...` |
| Koneksi (admin) | Fazri | `/api/admin/connections` |
| Job & queue | Fazri | `/api/jobs`, `/api/jobs/{id}` |
| User, group, grant, leader | Dzakwan | `/api/admin/users`, `/api/admin/groups` |
| Approval | Dzakwan | `/api/approvals` |
| Migration, history, rollback/forward | Zahran | `/api/databases/{id}/migrations`, `/api/migrations/{id}` |