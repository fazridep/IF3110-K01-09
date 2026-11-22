import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryRouter, RouterProvider } from "react-router";
import DatabasesPage from "./DatabasesPage";
import DatabaseDetailPage from "./DatabaseDetailPage";
import TablePage from "./TablePage";

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

const problem = (status: number, detail: string) =>
  new Response(JSON.stringify({ type: "urn:problem:x", title: "Error", status, detail, requestId: "r1" }), {
    status,
    headers: { "Content-Type": "application/problem+json" },
  });

function renderAt(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createMemoryRouter(
    [
      { path: "/databases", element: <DatabasesPage /> },
      { path: "/databases/:dbId", element: <DatabaseDetailPage /> },
      { path: "/databases/:dbId/tables/:schema/:table", element: <TablePage /> },
    ],
    { initialEntries: [path] },
  );
  render(
    <QueryClientProvider client={qc}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

const tableBody = (page: number) => ({
  database: { id: "d1", name: "toko", database: "toko_db", level: "view_only" },
  schema: "public",
  table: "barang",
  columns: [{ name: "id", dataType: "integer", nullable: false, default: "" }],
  constraints: [{ name: "barang_pkey", kind: "primary key", definition: "PRIMARY KEY (id)" }],
  indexes: [],
  rows: { columns: ["id", "nama"], rows: [[String(page), null]], total: 60, page, perPage: 50, pages: 2 },
});

describe("halaman database", () => {
  const fetchMock = vi.fn<typeof fetch>();

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock);
    fetchMock.mockReset();
  });
  afterEach(() => vi.unstubAllGlobals());

  it("menampilkan daftar database beserta level akses", async () => {
    fetchMock.mockResolvedValueOnce(json(200, { items: [{ id: "d1", name: "toko", database: "toko_db", level: "view_edit" }] }));
    renderAt("/databases");
    expect(await screen.findByText("toko")).toBeInTheDocument();
    expect(screen.getByText("View + Edit")).toBeInTheDocument();
  });

  it("menjelaskan kalau belum punya akses ke database mana pun", async () => {
    fetchMock.mockResolvedValueOnce(json(200, { items: [] }));
    renderAt("/databases");
    expect(await screen.findByText("Belum ada database")).toBeInTheDocument();
  });

  it("menampilkan pesan dari server waktu database target mati", async () => {
    fetchMock.mockResolvedValueOnce(problem(502, "Database ini lagi nggak bisa dihubungi."));
    renderAt("/databases/d1");
    expect(await screen.findByText("Database ini lagi nggak bisa dihubungi.")).toBeInTheDocument();
    expect(screen.getByRole("alert")).toBeInTheDocument();
  });

  it("menampilkan schema dan tabel dengan tautan yang di-encode", async () => {
    fetchMock.mockResolvedValueOnce(
      json(200, {
        id: "d1", name: "toko", database: "toko_db", level: "view_only",
        schemas: [{ name: "my schema", tables: [{ name: "barang", kind: "table", columns: 2 }] }],
      }),
    );
    renderAt("/databases/d1");
    const link = await screen.findByRole("link", { name: "barang" });
    expect(link).toHaveAttribute("href", "/databases/d1/tables/my%20schema/barang");
  });

  it("menampilkan isi tabel, NULL, dan pindah halaman", async () => {
    fetchMock.mockResolvedValueOnce(json(200, tableBody(1)));
    renderAt("/databases/d1/tables/public/barang");
    expect(await screen.findByText("NULL")).toBeInTheDocument();
    expect(screen.getByText("barang_pkey")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Sebelumnya" })).toBeDisabled();

    fetchMock.mockResolvedValueOnce(json(200, tableBody(2)));
    await userEvent.setup().click(screen.getByRole("button", { name: "Berikutnya" }));
    await waitFor(() => expect(String(fetchMock.mock.calls[1][0])).toContain("page=2"));
    expect(await screen.findByRole("button", { name: "Berikutnya" })).toBeDisabled();
  });
});
