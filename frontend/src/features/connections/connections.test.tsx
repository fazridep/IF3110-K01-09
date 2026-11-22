import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import ConnectionsPage from "./ConnectionsPage";

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

const problem = (status: number, detail: string, errors?: unknown) =>
  new Response(JSON.stringify({ type: "urn:problem:x", title: "Error", status, detail, errors, requestId: "r1" }), {
    status,
    headers: { "Content-Type": "application/problem+json" },
  });

const conn = {
  id: "c1", name: "toko", host: "db.local", port: 5432, database: "toko_db", username: "app",
  sslmode: "prefer", createdAt: "2026-11-08T08:00:00Z", updatedAt: "2026-11-08T08:00:00Z",
};

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <ConnectionsPage />
    </QueryClientProvider>,
  );
}

function sentBody(call: number) {
  return JSON.parse(String(fetchMock.mock.calls[call][1]?.body));
}

const fetchMock = vi.fn<typeof fetch>();

describe("halaman koneksi", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock);
    fetchMock.mockReset();
    document.cookie = "csrf=t; path=/";
  });
  afterEach(() => vi.unstubAllGlobals());

  it("menampilkan error per kolom waktu simpan koneksi baru", async () => {
    fetchMock.mockResolvedValueOnce(json(200, { items: [] }));
    renderPage();
    const user = userEvent.setup();
    await screen.findByText("Belum ada koneksi");
    await user.click(screen.getByRole("button", { name: "+ Koneksi Baru" }));

    fetchMock.mockResolvedValueOnce(problem(400, "host wajib diisi", [{ field: "host", detail: "host wajib diisi" }]));
    await user.type(screen.getByLabelText("Nama koneksi"), "toko");
    await user.click(screen.getByRole("button", { name: "Simpan" }));
    expect(await screen.findByText("host wajib diisi")).toBeInTheDocument();
  });

  it("form ubah nggak ngirim password lama atau field asing, dan tes pakai id", async () => {
    fetchMock.mockResolvedValueOnce(json(200, { items: [conn] }));
    renderPage();
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Ubah" }));
    expect(screen.getByLabelText("Host")).toHaveValue("db.local");
    expect(screen.getByLabelText("Password")).toHaveValue("");

    fetchMock.mockResolvedValueOnce(json(200, { ok: false, message: "username atau password ditolak" }));
    await user.click(screen.getByRole("button", { name: "Tes Koneksi" }));
    expect(await screen.findByText("username atau password ditolak")).toBeInTheDocument();
    expect(sentBody(1)).toMatchObject({ id: "c1", password: "", host: "db.local" });

    fetchMock.mockResolvedValueOnce(json(200, conn)).mockResolvedValueOnce(json(200, { items: [conn] }));
    await user.click(screen.getByRole("button", { name: "Simpan" }));
    await waitFor(() => expect(fetchMock.mock.calls[2][1]?.method).toBe("PUT"));
    expect(sentBody(2)).not.toHaveProperty("createdAt");
    expect(sentBody(2)).not.toHaveProperty("id");
  });

  it("menampilkan alasan waktu koneksi masih dipakai dan nggak bisa dihapus", async () => {
    fetchMock.mockResolvedValueOnce(json(200, { items: [conn] }));
    renderPage();
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Hapus" }));

    fetchMock.mockResolvedValueOnce(problem(409, "Koneksi ini masih punya riwayat migration."));
    const dialog = screen.getByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: "Hapus" }));
    expect(await screen.findByText("Koneksi ini masih punya riwayat migration.")).toBeInTheDocument();
  });

  it("menampilkan hasil tes koneksi yang tersimpan", async () => {
    fetchMock.mockResolvedValueOnce(json(200, { items: [conn] }));
    renderPage();
    const user = userEvent.setup();
    fetchMock.mockResolvedValueOnce(json(200, { ok: true, message: "Koneksi berhasil." }));
    await user.click(await screen.findByRole("button", { name: "Tes" }));
    expect(await screen.findByText("Koneksi berhasil.")).toBeInTheDocument();
  });
});
