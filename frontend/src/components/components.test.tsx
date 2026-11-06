import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider, useQuery } from "@tanstack/react-query";
import { QueryState } from "./QueryState";
import { DataTable } from "./DataTable";
import { StatusBadge, statusInfo } from "./StatusBadge";
import { EmptyState } from "./States";
import { ApiError } from "../lib/api";

function Harness({ fn }: { fn: () => Promise<string[]> }) {
  const q = useQuery({ queryKey: ["x"], queryFn: fn, retry: false });
  return (
    <QueryState query={q} isEmpty={(d) => d.length === 0} empty={<EmptyState title="Kosong" />}>
      {(d) => <p>isi: {d.join(",")}</p>}
    </QueryState>
  );
}

const renderQ = (fn: () => Promise<string[]>) =>
  render(
    <QueryClientProvider client={new QueryClient()}>
      <Harness fn={fn} />
    </QueryClientProvider>,
  );

describe("QueryState", () => {
  it("loading lalu data", async () => {
    renderQ(async () => ["a", "b"]);
    expect(screen.getByText("Memuat…")).toBeInTheDocument();
    expect(await screen.findByText("isi: a,b")).toBeInTheDocument();
  });

  it("kosong", async () => {
    renderQ(async () => []);
    expect(await screen.findByText("Kosong")).toBeInTheDocument();
  });

  it("error dengan tombol coba lagi", async () => {
    const fn = vi
      .fn<() => Promise<string[]>>()
      .mockRejectedValueOnce(new ApiError({ type: "urn:problem:internal", title: "Terjadi kesalahan di server", status: 500, requestId: "r1" }))
      .mockResolvedValueOnce(["ok"]);
    renderQ(fn);

    expect(await screen.findByRole("alert")).toHaveTextContent("request id: r1");
    await userEvent.setup().click(screen.getByRole("button", { name: "Coba lagi" }));
    expect(await screen.findByText("isi: ok")).toBeInTheDocument();
  });
});

describe("DataTable", () => {
  it("merender baris dan kolom, atau empty", () => {
    const { rerender } = render(
      <DataTable rows={[{ id: "1", name: "orders" }]} rowKey={(r) => r.id} columns={[{ header: "Nama", cell: (r) => r.name }]} />,
    );
    expect(screen.getByRole("columnheader", { name: "Nama" })).toBeInTheDocument();
    expect(screen.getByRole("cell", { name: "orders" })).toBeInTheDocument();

    rerender(<DataTable rows={[]} rowKey={(r: { id: string }) => r.id} columns={[]} empty={<p>tidak ada</p>} />);
    expect(screen.getByText("tidak ada")).toBeInTheDocument();
  });
});

describe("StatusBadge", () => {
  it("memetakan status alur migration ke label dan warna", () => {
    render(<StatusBadge status="running" />);
    expect(screen.getByText("Berjalan").className).toContain("is-live");
    expect(statusInfo("pending_review").tone).toBe("warn");
    expect(statusInfo("status_baru").label).toBe("status_baru");
  });
});