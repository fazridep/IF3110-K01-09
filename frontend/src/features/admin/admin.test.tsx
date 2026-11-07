import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryRouter, Outlet, RouterProvider } from "react-router";
import { AuthProvider } from "../../auth/AuthContext";
import { RequireAdmin, RequireAuth } from "../../auth/RequireAuth";
import UsersPage from "./UsersPage";
import GroupsPage from "./GroupsPage";
import GroupDetailPage from "./GroupDetailPage";

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });

const problem = (status: number, type: string, detail: string, errors?: unknown) =>
  new Response(JSON.stringify({ type, title: "Error", status, detail, errors, requestId: "req-1" }), {
    status,
    headers: { "Content-Type": "application/problem+json" },
  });

function renderWithRouter(initial: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createMemoryRouter(
    [
      {
        element: (
          <RequireAuth>
            <RequireAdmin>
              <Outlet />
            </RequireAdmin>
          </RequireAuth>
        ),
        children: [
          { path: "/admin/users", element: <UsersPage /> },
          { path: "/admin/groups", element: <GroupsPage /> },
          { path: "/admin/groups/:groupId", element: <GroupDetailPage /> },
        ],
      },
    ],
    { initialEntries: [initial] },
  );
  render(
    <QueryClientProvider client={qc}>
      <AuthProvider>
        <RouterProvider router={router} />
      </AuthProvider>
    </QueryClientProvider>,
  );
  return router;
}

describe("admin pages", () => {
  const fetchMock = vi.fn<typeof fetch>();

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock);
    fetchMock.mockReset();
    document.cookie = "csrf=t; path=/";
  });

  afterEach(() => vi.unstubAllGlobals());

  it("UsersPage menampilkan daftar user dan error validasi saat tambah user", async () => {
    fetchMock
      .mockResolvedValueOnce(json(200, { id: "u-admin", username: "admin", isPlatformAdmin: true }))
      .mockResolvedValueOnce(
        json(200, {
          items: [
            {
              id: "u-admin",
              username: "admin",
              isPlatformAdmin: true,
              disabled: false,
              createdAt: "2026-11-06T08:00:00Z",
              updatedAt: "2026-11-06T08:00:00Z",
            },
            {
              id: "u-budi",
              username: "budi",
              isPlatformAdmin: false,
              disabled: false,
              createdAt: "2026-11-06T08:00:00Z",
              updatedAt: "2026-11-06T08:00:00Z",
            },
          ],
        }),
      );

    renderWithRouter("/admin/users");
    expect(await screen.findByText("budi")).toBeInTheDocument();
    expect(screen.getByText("Platform Admin")).toBeInTheDocument();

    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "+ Tambah User" }));

    fetchMock.mockResolvedValueOnce(
      problem(409, "urn:problem:conflict", "Username sudah dipakai.", [
        { field: "username", detail: "sudah dipakai" },
      ]),
    );
    await user.type(screen.getByLabelText("Username"), "budi");
    await user.type(screen.getByLabelText("Password"), "rahasia");
    await user.click(screen.getByRole("button", { name: "Tambah User" }));

    expect(await screen.findByText("sudah dipakai")).toBeInTheDocument();
  });

  it("GroupsPage dan GroupDetailPage menampilkan leader dan bisa menunjuk leader baru", async () => {
    fetchMock
      .mockResolvedValueOnce(json(200, { id: "u-admin", username: "admin", isPlatformAdmin: true }))
      .mockResolvedValueOnce(
        json(200, {
          items: [
            {
              id: "g-1",
              name: "backend-team",
              description: "Tim Backend",
              leaderId: "u-budi",
              leaderUsername: "budi",
              memberCount: 2,
              createdAt: "2026-11-06T08:00:00Z",
              updatedAt: "2026-11-06T08:00:00Z",
            },
          ],
        }),
      );

    renderWithRouter("/admin/groups");
    expect(await screen.findByText("backend-team")).toBeInTheDocument();
    expect(screen.getByText("budi")).toBeInTheDocument();

    fetchMock.mockResolvedValueOnce(
      json(200, {
        group: {
          id: "g-1",
          name: "backend-team",
          description: "Tim Backend",
          leaderId: null,
          leaderUsername: "",
          memberCount: 1,
          createdAt: "2026-11-06T08:00:00Z",
          updatedAt: "2026-11-06T08:00:00Z",
        },
        members: [
          {
            userId: "u-budi",
            username: "budi",
            isLeader: false,
            addedAt: "2026-11-06T08:00:00Z",
          },
        ],
        nonMembers: [],
        grants: [],
        databases: [],
      }),
    );

    const user = userEvent.setup();
    await user.click(screen.getByRole("link", { name: "Kelola" }));

    expect(await screen.findByText("Informasi & Leader Group")).toBeInTheDocument();
    const setLeaderBtn = await screen.findByRole("button", { name: "Jadikan Leader" });

    fetchMock
      .mockResolvedValueOnce(
        json(200, {
          id: "g-1",
          name: "backend-team",
          description: "Tim Backend",
          leaderId: "u-budi",
          leaderUsername: "budi",
          memberCount: 1,
          createdAt: "2026-11-06T08:00:00Z",
          updatedAt: "2026-11-06T08:00:00Z",
        }),
      )
      .mockResolvedValueOnce(
        json(200, {
          group: {
            id: "g-1",
            name: "backend-team",
            description: "Tim Backend",
            leaderId: "u-budi",
            leaderUsername: "budi",
            memberCount: 1,
            createdAt: "2026-11-06T08:00:00Z",
            updatedAt: "2026-11-06T08:00:00Z",
          },
          members: [
            {
              userId: "u-budi",
              username: "budi",
              isLeader: true,
              addedAt: "2026-11-06T08:00:00Z",
            },
          ],
          nonMembers: [],
          grants: [],
          databases: [],
        }),
      );

    await user.click(setLeaderBtn);
    expect(await screen.findByText("Leader: budi")).toBeInTheDocument();
  });

  it("Menolak akses halaman admin jika user bukan Platform Admin", async () => {
    fetchMock.mockResolvedValueOnce(
      json(200, { id: "u-biasa", username: "biasa", isPlatformAdmin: false }),
    );
    renderWithRouter("/admin/users");
    expect(await screen.findByText("Khusus Platform Admin")).toBeInTheDocument();
  });
});

