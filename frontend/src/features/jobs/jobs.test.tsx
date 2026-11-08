import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryRouter, RouterProvider } from "react-router";
import JobsPage from "./JobsPage";
import JobDetailPage from "./JobDetailPage";
import { isActive, type Job } from "./types";

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

const job = (over: Partial<Job> = {}): Job => ({
  id: "j1", databaseId: "d1", databaseName: "toko", migrationId: "m1", migrationVersion: 3,
  description: "tambah kolom", approvalId: "a1", action: "apply", resumesJobId: null, status: "success",
  attempt: 1, maxAttempts: 3, error: "", batchesCommitted: 0, rowsCommitted: 0, createdBy: "budi",
  createdAt: "2026-11-08T08:00:00Z", updatedAt: "2026-11-08T08:00:05Z", runAfter: "2026-11-08T08:00:00Z",
  startedAt: "2026-11-08T08:00:01Z", finishedAt: "2026-11-08T08:00:05Z", ...over,
});

function renderAt(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createMemoryRouter(
    [
      { path: "/jobs", element: <JobsPage /> },
      { path: "/jobs/:jobId", element: <JobDetailPage /> },
    ],
    { initialEntries: [path] },
  );
  render(
    <QueryClientProvider client={qc}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

describe("halaman job", () => {
  const fetchMock = vi.fn<typeof fetch>();

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock);
    fetchMock.mockReset();
    document.cookie = "csrf=t; path=/";
  });
  afterEach(() => vi.unstubAllGlobals());

  it("menampilkan daftar job, memfilter status, dan memuat halaman berikutnya", async () => {
    fetchMock.mockResolvedValueOnce(json(200, { items: [job()], nextCursor: "7" }));
    renderAt("/jobs");
    const user = userEvent.setup();
    expect(await screen.findByText("tambah kolom")).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "Daftar job migration" })).toHaveTextContent("Sukses");

    fetchMock.mockResolvedValueOnce(json(200, { items: [job({ id: "j0", description: "lebih lama" })] }));
    await user.click(screen.getByRole("button", { name: "Muat lebih banyak" }));
    expect(await screen.findByText("lebih lama")).toBeInTheDocument();
    expect(String(fetchMock.mock.calls[1][0])).toContain("cursor=7");

    fetchMock.mockResolvedValueOnce(json(200, { items: [] }));
    await user.click(screen.getByRole("button", { name: "Gagal" }));
    expect(await screen.findByText("Belum ada job")).toBeInTheDocument();
    expect(String(fetchMock.mock.calls[2][0])).toContain("status=failed");
  });

  it("detail menampilkan progres batch dan status partial", async () => {
    fetchMock.mockResolvedValueOnce(json(200, job({ status: "partial", batchesCommitted: 2, rowsCommitted: 20000, error: "batch 3 gagal" })));
    renderAt("/jobs/j1");
    expect(await screen.findByText("20.000")).toBeInTheDocument();
    expect(screen.getByText("batch 3 gagal")).toBeInTheDocument();
    expect(screen.getByText(/ditahan sampai job ini dilanjutkan/)).toBeInTheDocument();
  });

  it("job uncertain bisa diperiksa ulang", async () => {
    fetchMock.mockResolvedValueOnce(json(200, job({ status: "uncertain" })));
    renderAt("/jobs/j1");
    const user = userEvent.setup();
    const btn = await screen.findByRole("button", { name: "Periksa ulang" });

    fetchMock.mockResolvedValueOnce(json(200, job({ status: "success" })));
    await user.click(btn);
    expect(await screen.findByText("Sukses")).toBeInTheDocument();
    expect(fetchMock.mock.calls[1][0]).toBe("/api/jobs/j1/recheck");
    expect(fetchMock.mock.calls[1][1]?.method).toBe("POST");
  });

  it("job selesai nggak dipantau lagi, job aktif dipantau", () => {
    expect(isActive("running")).toBe(true);
    expect(isActive("uncertain")).toBe(true);
    expect(isActive("partial")).toBe(false);
    expect(isActive("success")).toBe(false);
  });
});
