# Frontend SPA

React + Vite + TypeScript di `frontend/`. Butuh Node.js 22+.

## Menjalankan

```bash
make frontend-install   # sekali, atau tiap package-lock.json berubah
```

Development (hot reload):

```bash
make run            # terminal 1, backend di :8080
make frontend-dev   # terminal 2, buka http://localhost:5173
```

Production / demo:

```bash
make frontend-build   # hasil di frontend/dist
make run              # SPA + API di http://localhost:8080
```

Test: `make frontend-test`. Cek tipe: `cd frontend && npm run typecheck`.

## Info penting

- Menu yang halamannya belum jadi cuma nampilin "On progress implementasi".
- Halaman Go template lama masih jalan selama transisi. Bukanya langsung
  lewat URL di http://localhost:8080, misal `/databases` atau `/admin/users`.
- Login pakai cookie `sid` HttpOnly. Jangan simpan token atau data sensitif
  di `localStorage`.
- Semua request ke backend lewat `api()` di `src/lib/api.ts`, jangan `fetch`
  langsung. CSRF, error problem+json, dan 401 sudah ditangani di situ.
- Format API (error, list, status code) ada di [api.md](api.md).
- Warna cuma lewat `var(--...)` di `src/styles/tokens.css`.
- Wajib responsif: cek di lebar 390 px. Tabel dan SQL boleh scroll di dalam
  komponennya, halaman nggak boleh scroll ke samping.

## Komponen bersama

Ada di `src/components/`, tinggal import:

| Komponen | Buat |
|----------|------|
| `PageHeader` | judul halaman |
| `Panel` | kotak konten, props `title`, `actions`, `footer`, `flush` |
| `QueryState` | wajib buat data dari API: loading, error, kosong, ada data |
| `DataTable` | tabel yang scroll di dalam komponennya |
| `Skeleton`, `SkeletonTable` | placeholder waktu loading |
| `EmptyState`, `ErrorState`, `Alert` | data kosong, gagal, pesan |
| `StatusBadge` | status migration/job (`draft`, `pending_review`, `running`, `success`, `failed`, `partial`, ...) |
| `Button` | varian `accent`, `line`, `ghost`, `danger`, prop `loading` |
| `Field` | label + input + pesan error |
| `Dialog`, `ConfirmDialog` | modal dan konfirmasi aksi penting |
| `CodeBlock` | tampilan SQL dengan nomor baris |

Contoh halaman list:

```tsx
export default function GroupsPage() {
  const groups = useQuery({ queryKey: ["groups"], queryFn: () => api<List<Group>>("/api/admin/groups") });
  return (
    <>
      <PageHeader title="Groups" />
      <Panel flush>
        <QueryState query={groups} skeleton={<SkeletonTable />} isEmpty={(d) => d.items.length === 0}
          empty={<EmptyState title="Belum ada group" />}>
          {(d) => <DataTable rows={d.items} rowKey={(g) => g.id} columns={[{ header: "Nama", cell: (g) => g.name }]} />}
        </QueryState>
      </Panel>
    </>
  );
}
```

Error validasi per field dari server: `(err as ApiError).fieldError("name")`,
masukin ke `<Field error=...>`.

## Yang harus dikerjakan tiap orang

Halaman dibuat di folder masing-masing biar nggak bentrok:

| Orang | Folder | Halaman |
|-------|--------|---------|
| Dzakwan | `src/features/admin/`, `src/features/approvals/` | users, groups (+ leader), inbox approval |
| Fazri | `src/features/databases/`, `src/features/connections/`, `src/features/jobs/` | viewer database, koneksi, status job |
| Zahran | `src/features/migrations/` | daftar, form SQL up/down, history + hide rollback, rollback/forward, batch |

Langkahnya:

1. Bikin endpoint JSON di package Go sendiri, daftarkan di `RegisterAPI` di
   `cmd/server/server.go`.
2. Bikin halaman di folder sendiri. Ambil data pakai `useQuery` + `api()`,
   tampilkan lewat `QueryState` (loading, error, kosong).
3. Daftarkan route di `src/app/router.tsx`, lalu ubah `ready: true` di
   `src/layout/nav.ts`.
4. Hapus route Go template lama fitur itu dari `RegisterPages` di
   `cmd/server/server.go`, plus template-nya di `internal/web/templates/`.

Contoh route:

```tsx
{ path: "admin/groups", lazy: { Component: async () => (await import("../features/admin/GroupsPage")).default } },
```

Buat status job yang berjalan, pakai polling:

```ts
useQuery({
  queryKey: ["jobs", id],
  queryFn: () => api<Job>(`/api/jobs/${id}`),
  refetchInterval: (q) => (["success", "failed"].includes(q.state.data?.status ?? "") ? false : 2000),
});
```