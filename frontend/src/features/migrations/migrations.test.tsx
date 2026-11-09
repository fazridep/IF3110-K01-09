import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryRouter, RouterProvider } from "react-router";
import MigrationsPage from "./MigrationsPage";
import NewMigrationPage from "./NewMigrationPage";
import MigrationDetailPage from "./MigrationDetailPage";
import type { Execution, MigrationDetail } from "./types";

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": status >= 400 ? "application/problem+json" : "application/json" },
  });

const migration = (over: Partial<MigrationDetail> = {}): MigrationDetail => ({
  id: "m1", databaseId: "d1", version: 2, description: "tambah tabel produk",
  upSql: "CREATE TABLE produk (id INT);", downSql: "DROP TABLE produk;", irreversible: false,
  irreversibleReason: "", mode: "normal", batch: null, status: "success", error: "", createdBy: "budi",
  createdAt: "2026-11-08T08:00:00Z", updatedAt: "2026-11-08T08:00:00Z", executedBy: "budi",
  startedAt: "2026-11-08T08:01:00Z", finishedAt: "2026-11-08T08:01:02Z", upExecutionId: "e2",
  canEdit: true, allowedActions: ["rollback"], blockedReason: "", ...over,
});

const execution = (over: Partial<Execution> = {}): Execution => ({
  id: "e1aaaaaaaa", migrationId: "m1", version: 2, description: "tambah tabel produk", action: "apply",
  revertsExecutionId: null, jobId: "j1aaaaaaaa", status: "success", sql: "CREATE TABLE produk (id INT);",
  mode: "normal", batchSize: 0, batchesCommitted: 0, rowsCommitted: 0, cursor: "", error: "",
  executedBy: "budi", startedAt: "2026-11-08T08:01:00Z", finishedAt: "2026-11-08T08:01:02Z",
  committed: true, committedAt: "2026-11-08T08:01:02Z", ...over,
});

type Route = (url: URL, init?: RequestInit) => Response | undefined;

function renderAt(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createMemoryRouter(
    [
      { path: "/databases/:dbId/migrations", element: <MigrationsPage /> },
      { path: "/databases/:dbId/migrations/new", element: <NewMigrationPage /> },
      { path: "/migrations/:id", element: <MigrationDetailPage /> },
      { path: "/jobs/:jobId", element: <p>halaman job</p> },
    ],
    { initialEntries: [path] },
  );
  render(
    <QueryClientProvider client={qc}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return router;
}

describe("halaman migration", () => {
  const fetchMock = vi.fn<typeof fetch>();
  let route: Route;

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock);
    fetchMock.mockReset();
    fetchMock.mockImplementation(async (input, init) => {
      const url = new URL(String(input), "http://localhost");
      if (url.pathname === "/api/databases") {
        return json(200, { items: [{ id: "d1", name: "toko", database: "toko_db", level: "view_edit" }] });
      }
      return route(url, init) ?? json(404, { type: "urn:problem:not-found", title: "Tidak ditemukan", status: 404 });
    });
    document.cookie = "csrf=t; path=/";
  });
  afterEach(() => vi.unstubAllGlobals());

  it("daftar dan riwayat, hide rollback meminta riwayat yang difilter", async () => {
    route = (url) => {
      if (url.pathname === "/api/databases/d1/migrations") return json(200, { items: [migration()] });
      if (url.pathname === "/api/databases/d1/history") {
        if (url.searchParams.get("hideRollback") === "true") return json(200, { items: [] });
        return json(200, {
          items: [
            execution(),
            execution({ id: "e2bbbbbbbb", action: "rollback", revertsExecutionId: "e1aaaaaaaa", sql: "DROP TABLE produk;" }),
          ],
        });
      }
    };
    renderAt("/databases/d1/migrations");
    const user = userEvent.setup();

    expect(await screen.findByRole("link", { name: "tambah tabel produk" })).toBeInTheDocument();
    const history = await screen.findByRole("region", { name: "Riwayat eksekusi migration" });
    expect(within(history).getByText("membatalkan E e1aaaaaa")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Migration baru" })).toBeInTheDocument();

    await user.click(screen.getByRole("checkbox", { name: "Hide rollback" }));
    expect(await screen.findByText("Belum ada eksekusi")).toBeInTheDocument();
    expect(fetchMock.mock.calls.some(([u]) => String(u).includes("hideRollback=true"))).toBe(true);
  });

  it("form draft menampilkan error per kolom dari server", async () => {
    let body: Record<string, unknown> | undefined;
    route = (url, init) => {
      if (url.pathname === "/api/databases/d1/migrations" && init?.method === "POST") {
        body = JSON.parse(String(init.body));
        return json(400, {
          type: "urn:problem:invalid-input", title: "Input tidak valid", status: 400,
          detail: "SQL down wajib diisi", errors: [{ field: "downSql", detail: "SQL down wajib diisi, atau tandai irreversible beserta alasannya" }],
        });
      }
    };
    renderAt("/databases/d1/migrations/new");
    const user = userEvent.setup();

    await user.type(await screen.findByLabelText("Deskripsi"), "tabel baru");
    await user.type(screen.getByLabelText("SQL up"), "CREATE TABLE baru (id INT);");
    await user.click(screen.getByRole("button", { name: "Simpan draft" }));

    expect(await screen.findByText(/tandai irreversible beserta alasannya/)).toBeInTheDocument();
    expect(body).toMatchObject({ description: "tabel baru", mode: "normal", batch: null, irreversible: false });
  });

  it("mode batch mengirim konfigurasi batch", async () => {
    let body: Record<string, unknown> | undefined;
    route = (url, init) => {
      if (url.pathname === "/api/databases/d1/migrations" && init?.method === "POST") {
        body = JSON.parse(String(init.body));
        return json(201, migration({ id: "m9", status: "draft" }));
      }
      if (url.pathname === "/api/migrations/m9") return json(200, migration({ id: "m9", status: "draft", allowedActions: ["apply"] }));
      if (url.pathname === "/api/databases/d1/history") return json(200, { items: [] });
      if (url.pathname === "/api/approvals") return json(200, { items: [] });
    };
    const router = renderAt("/databases/d1/migrations/new");
    const user = userEvent.setup();

    await user.type(await screen.findByLabelText("Deskripsi"), "naikkan harga");
    await user.selectOptions(screen.getByLabelText("Mode eksekusi"), "batch");
    await user.type(screen.getByLabelText("Tabel"), "orders_large");
    await user.clear(screen.getByLabelText("Ukuran batch"));
    await user.type(screen.getByLabelText("Ukuran batch"), "5000");
    await user.type(screen.getByLabelText("SQL up"), "UPDATE orders_large o SET amount = 1 FROM batch_rows b WHERE o.id = b.id;");
    await user.type(screen.getByLabelText("SQL down"), "UPDATE orders_large o SET amount = b.amount FROM batch_rows b WHERE o.id = b.id;");
    await user.click(screen.getByRole("button", { name: "Simpan draft" }));

    await waitFor(() => expect(router.state.location.pathname).toBe("/migrations/m9"));
    expect(body).toMatchObject({ mode: "batch", batch: { table: "orders_large", key: "id", size: 5000, backup: true } });
  });

  it("detail: ajukan rollback lalu jalankan approval yang sudah disetujui", async () => {
    const posts: { path: string; body: unknown }[] = [];
    route = (url, init) => {
      if (init?.method === "POST") {
        posts.push({ path: url.pathname, body: JSON.parse(String(init.body)) });
        if (url.pathname === "/api/approvals") return json(201, { id: "a2" });
        if (url.pathname === "/api/jobs") return json(202, { jobId: "j7", status: "queued", statusUrl: "/api/jobs/j7" });
      }
      if (url.pathname === "/api/migrations/m1") return json(200, migration());
      if (url.pathname === "/api/databases/d1/history") return json(200, { items: [execution()] });
      if (url.pathname === "/api/approvals") {
        return json(200, {
          items: [
            { id: "a1", migrationId: "m1", action: "apply", status: "approved", approvedCount: 1, totalRequired: 1, requestedBy: "budi", createdAt: "2026-11-08T08:00:00Z" },
            { id: "a0", migrationId: "lain", action: "apply", status: "approved", approvedCount: 1, totalRequired: 1, requestedBy: "budi", createdAt: "2026-11-08T08:00:00Z" },
          ],
        });
      }
    };
    const router = renderAt("/migrations/m1");
    const user = userEvent.setup();

    await user.click(await screen.findByRole("button", { name: "Ajukan Rollback" }));
    await waitFor(() => expect(posts).toContainEqual({ path: "/api/approvals", body: { migrationId: "m1", action: "rollback" } }));

    const approvals = await screen.findByRole("region", { name: "Approval migration ini" });
    expect(within(approvals).getAllByRole("row")).toHaveLength(2); // header + approval milik m1 saja
    await user.click(within(approvals).getByRole("button", { name: "Jalankan" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/jobs/j7"));
    expect(posts).toContainEqual({ path: "/api/jobs", body: { approvalId: "a1" } });
  });

  it("view only nggak dapat tombol aksi", async () => {
    route = (url) => {
      if (url.pathname === "/api/migrations/m1") return json(200, migration({ canEdit: false }));
      if (url.pathname === "/api/databases/d1/history") return json(200, { items: [] });
      if (url.pathname === "/api/approvals") return json(200, { items: [] });
    };
    renderAt("/migrations/m1");
    expect(await screen.findByText(/Akses kamu View Only/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Ajukan/ })).not.toBeInTheDocument();
  });

  it("draft yang balik karena gagal sebelum SQL jalan diberi penjelasan", async () => {
    route = (url) => {
      if (url.pathname === "/api/migrations/m1") {
        return json(200, migration({ status: "draft", error: "koneksi ke database target gagal", allowedActions: ["apply"] }));
      }
      if (url.pathname === "/api/databases/d1/history") return json(200, { items: [execution({ status: "failed", committed: false })] });
      if (url.pathname === "/api/approvals") return json(200, { items: [] });
    };
    renderAt("/migrations/m1");
    expect(await screen.findByText(/gagal sebelum SQL sempat dijalankan/)).toBeInTheDocument();
    expect(screen.getByText("koneksi ke database target gagal")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Ajukan Apply" })).toBeInTheDocument();
  });

  it("ubah draft cuma mengirim field definisi, Ajukan nonaktif selama form terbuka", async () => {
    let body: Record<string, unknown> | undefined;
    let current = migration({ status: "draft", allowedActions: ["apply"] });
    route = (url, init) => {
      if (url.pathname === "/api/migrations/m1" && init?.method === "PUT") {
        body = JSON.parse(String(init.body));
        current = { ...current, description: String(body?.description) };
        return json(200, current);
      }
      if (url.pathname === "/api/migrations/m1") return json(200, current);
      if (url.pathname === "/api/databases/d1/history") return json(200, { items: [] });
      if (url.pathname === "/api/approvals") return json(200, { items: [] });
      if (url.pathname === "/api/databases/d1/migrations") return json(200, { items: [current] });
    };
    renderAt("/migrations/m1");
    const user = userEvent.setup();

    await user.click(await screen.findByRole("button", { name: "Ubah draft" }));
    expect(screen.getByRole("button", { name: "Ajukan Apply" })).toBeDisabled();
    expect(screen.getByText("Simpan atau batalkan perubahan draft dulu.")).toBeInTheDocument();

    const desc = screen.getByLabelText("Deskripsi");
    await user.clear(desc);
    await user.type(desc, "tabel produk v2");
    await user.click(screen.getByRole("button", { name: "Simpan perubahan" }));

    expect(await screen.findByRole("heading", { name: "v2 tabel produk v2" })).toBeInTheDocument();
    expect(Object.keys(body ?? {}).sort()).toEqual(
      ["batch", "description", "downSql", "irreversible", "irreversibleReason", "mode", "upSql"],
    );
    expect(screen.getByRole("button", { name: "Ajukan Apply" })).toBeEnabled();
  });

  it("error field yang nggak ada di form tetap ditampilkan", async () => {
    route = (url, init) => {
      if (url.pathname === "/api/databases/d1/migrations" && init?.method === "POST") {
        return json(400, {
          type: "urn:problem:invalid-input", title: "Input tidak valid", status: 400,
          detail: "Ada field yang tidak dikenal.", errors: [{ field: "id", detail: "field tidak dikenal" }],
        });
      }
    };
    renderAt("/databases/d1/migrations/new");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Simpan draft" }));
    expect(await screen.findByText("Ada field yang tidak dikenal.")).toBeInTheDocument();
  });

  it("draft yang ditahan menampilkan alasannya", async () => {
    route = (url) => {
      if (url.pathname === "/api/migrations/m1") {
        return json(200, migration({ status: "draft", allowedActions: [], blockedReason: "v7 belum tuntas (partial), selesaikan dulu sebelum menjalankan migration baru" }));
      }
      if (url.pathname === "/api/databases/d1/history") return json(200, { items: [] });
      if (url.pathname === "/api/approvals") return json(200, { items: [] });
    };
    renderAt("/migrations/m1");
    expect(await screen.findByText("Draft ini belum bisa diajukan")).toBeInTheDocument();
    expect(screen.getByText(/v7 belum tuntas \(partial\)/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Ajukan/ })).not.toBeInTheDocument();
  });
});
