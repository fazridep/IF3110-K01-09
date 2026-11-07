import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryRouter, Outlet, RouterProvider } from "react-router";
import { AuthProvider } from "../../auth/AuthContext";
import { RequireAuth } from "../../auth/RequireAuth";
import ApprovalsPage from "./ApprovalsPage";

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });

const problem = (status: number, type: string, detail: string, errors?: unknown) =>
  new Response(JSON.stringify({ type, title: "Error", status, detail, errors, requestId: "req-9" }), {
    status,
    headers: { "Content-Type": "application/problem+json" },
  });

function renderApprovals() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createMemoryRouter(
    [
      {
        element: (
          <RequireAuth>
            <Outlet />
          </RequireAuth>
        ),
        children: [{ path: "/approvals", element: <ApprovalsPage /> }],
      },
    ],
    { initialEntries: ["/approvals"] },
  );
  render(
    <QueryClientProvider client={qc}>
      <AuthProvider>
        <RouterProvider router={router} />
      </AuthProvider>
    </QueryClientProvider>,
  );
}

const samplePending = {
  id: "ap-1",
  migrationId: "mig-1",
  databaseId: "db-1",
  databaseName: "prod_orders",
  version: 3,
  description: "add customer_idx",
  sql: "CREATE INDEX idx_orders_customer ON orders (customer_id);",
  action: "apply",
  status: "pending_review",
  requestedById: "u-editor",
  requestedBy: "zahran",
  rejectionReason: "",
  createdAt: "2026-11-06T09:00:00Z",
  updatedAt: "2026-11-06T09:00:00Z",
  resolvedAt: null,
  approvedCount: 1,
  totalRequired: 2,
  canDecide: true,
  myLeaderGroups: ["analytics"],
  approvals: [
    {
      groupId: "g-1",
      groupName: "backend",
      accessLevel: "view_edit",
      leaderId: "u-budi",
      leaderUsername: "budi",
      decision: "approved",
      decidedBy: "budi",
      note: "OK dari backend",
      decidedAt: "2026-11-06T09:05:00Z",
    },
    {
      groupId: "g-2",
      groupName: "analytics",
      accessLevel: "view_only",
      leaderId: "u-siti",
      leaderUsername: "siti",
      decision: "pending_review",
      decidedBy: "",
      note: "",
      decidedAt: null,
    },
  ],
};

describe("ApprovalsPage", () => {
  const fetchMock = vi.fn<typeof fetch>();

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock);
    fetchMock.mockReset();
    document.cookie = "csrf=t; path=/";
  });

  afterEach(() => vi.unstubAllGlobals());

  it("menampilkan daftar inbox approval, detail SQL, dan proses approve sampai quorum terpenuhi", async () => {
    fetchMock
      .mockResolvedValueOnce(json(200, { id: "u-siti", username: "siti", isPlatformAdmin: false }))
      .mockResolvedValueOnce(json(200, { items: [samplePending] }));

    renderApprovals();
    expect(await screen.findByText("prod_orders")).toBeInTheDocument();
    expect(screen.getByText("1/2 group setuju")).toBeInTheDocument();

    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Tinjau SQL" }));
    expect(await screen.findByText("CREATE INDEX idx_orders_customer ON orders (customer_id);")).toBeInTheDocument();
    expect(screen.getByText("OK dari backend")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Tutup" }));
    await user.click(screen.getByRole("button", { name: "Setujui" }));

    const approvedItem = {
      ...samplePending,
      status: "approved",
      approvedCount: 2,
      totalRequired: 2,
      canDecide: false,
    };
    fetchMock
      .mockResolvedValueOnce(json(200, approvedItem))
      .mockResolvedValueOnce(json(200, { items: [approvedItem] }));

    await user.type(screen.getByLabelText("Catatan (Opsional)"), "Aman dari sisi analytics");
    await user.click(screen.getByRole("button", { name: "Konfirmasi Setuju" }));

    expect(await screen.findByText("2/2 group setuju")).toBeInTheDocument();
  });

  it("menampilkan error validasi alasan saat menolak approval tanpa alasan", async () => {
    fetchMock
      .mockResolvedValueOnce(json(200, { id: "u-siti", username: "siti", isPlatformAdmin: false }))
      .mockResolvedValueOnce(json(200, { items: [samplePending] }));

    renderApprovals();
    expect(await screen.findByText("prod_orders")).toBeInTheDocument();

    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Tolak" }));

    fetchMock.mockResolvedValueOnce(
      problem(400, "urn:problem:invalid-input", "Alasan penolakan wajib diisi.", [
        { field: "reason", detail: "wajib diisi" },
      ]),
    );
    await user.click(screen.getByRole("button", { name: "Tolak Permintaan" }));

    expect(await screen.findByText("wajib diisi")).toBeInTheDocument();
  });
});

