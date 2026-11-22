import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryRouter, RouterProvider } from "react-router";
import { AuthProvider, useAuth } from "./AuthContext";
import { RequireAuth } from "./RequireAuth";
import LoginPage, { nextFor, safeNext } from "./LoginPage";

const problem = (status: number, type: string, detail: string, errors?: unknown) =>
  new Response(JSON.stringify({ type, title: "x", status, detail, errors, requestId: "req-7" }), {
    status,
    headers: { "Content-Type": "application/problem+json" },
  });

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

function LogoutButton() {
  const { logout } = useAuth();
  return <button onClick={() => void logout()}>Logout</button>;
}

function renderApp(initial: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createMemoryRouter(
    [
      { path: "/login", element: <LoginPage /> },
      {
        path: "/",
        element: (
          <RequireAuth>
            <p>halaman rahasia</p>
          </RequireAuth>
        ),
      },
      {
        path: "/admin/users",
        element: (
          <RequireAuth>
            <LogoutButton />
          </RequireAuth>
        ),
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

describe("login", () => {
  const fetchMock = vi.fn<typeof fetch>();
  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock);
    fetchMock.mockReset();
    document.cookie = "csrf=t; path=/";
  });
  afterEach(() => vi.unstubAllGlobals());

  it("belum login diarahkan ke /login, lalu login berhasil masuk ke halaman", async () => {
    fetchMock.mockResolvedValueOnce(problem(401, "urn:problem:unauthenticated", "Silakan login dulu."));
    const router = renderApp("/");

    expect(await screen.findByRole("heading", { name: "Masuk ke platform" })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/login");

    fetchMock.mockResolvedValueOnce(json(201, { id: "u1", username: "budi", isPlatformAdmin: false }));
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Username"), "budi");
    await user.type(screen.getByLabelText("Password"), "rahasia");
    await user.click(screen.getByRole("button", { name: "Login" }));

    expect(await screen.findByText("halaman rahasia")).toBeInTheDocument();
    const [url, init] = fetchMock.mock.calls[1];
    expect(url).toBe("/api/session");
    expect(init?.method).toBe("POST");
    expect(init?.body).toBe(JSON.stringify({ username: "budi", password: "rahasia" }));
  });

  it("login gagal menampilkan pesan dari server dan mengosongkan password", async () => {
    fetchMock.mockResolvedValueOnce(problem(401, "urn:problem:unauthenticated", "Silakan login dulu."));
    renderApp("/login");
    await screen.findByLabelText("Username");

    fetchMock.mockResolvedValueOnce(problem(401, "urn:problem:invalid-credentials", "Username atau password salah."));
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Username"), "budi");
    await user.type(screen.getByLabelText("Password"), "salah");
    await user.click(screen.getByRole("button", { name: "Login" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Username atau password salah.");
    expect(screen.getByRole("alert")).not.toHaveTextContent("req-7");
    expect(screen.getByLabelText("Password")).toHaveValue("");
  });

  it("error validasi ditampilkan di field-nya", async () => {
    fetchMock.mockResolvedValueOnce(problem(401, "urn:problem:unauthenticated", ""));
    renderApp("/login");
    await screen.findByLabelText("Username");

    fetchMock.mockResolvedValueOnce(
      problem(400, "urn:problem:invalid-input", "Username dan password wajib diisi.", [
        { field: "username", detail: "wajib diisi" },
        { field: "password", detail: "wajib diisi" },
      ]),
    );
    await userEvent.setup().click(screen.getByRole("button", { name: "Login" }));

    await screen.findByRole("alert");
    expect(screen.getByLabelText("Username")).toHaveAttribute("aria-invalid", "true");
    expect(screen.getAllByText("wajib diisi")).toHaveLength(2);
  });
});

describe("ganti akun", () => {
  const fetchMock = vi.fn<typeof fetch>();
  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock);
    fetchMock.mockReset();
    document.cookie = "csrf=t; path=/";
  });
  afterEach(() => vi.unstubAllGlobals());

  it("logout manual ke /login tanpa membawa halaman terakhir", async () => {
    fetchMock.mockResolvedValueOnce(json(200, { id: "a1", username: "admin", isPlatformAdmin: true }));
    const router = renderApp("/admin/users");
    await screen.findByRole("button", { name: "Logout" });

    fetchMock.mockResolvedValueOnce(new Response(null, { status: 204 }));
    await userEvent.setup().click(screen.getByRole("button", { name: "Logout" }));

    expect(await screen.findByRole("heading", { name: "Masuk ke platform" })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/login");
    expect(router.state.location.search).toBe("");
  });

  it("next ke halaman admin diabaikan kalau yang login bukan admin", async () => {
    fetchMock.mockResolvedValueOnce(problem(401, "urn:problem:unauthenticated", ""));
    const router = renderApp("/login?next=%2Fadmin%2Fusers");
    await screen.findByLabelText("Username");

    fetchMock.mockResolvedValueOnce(json(201, { id: "v1", username: "viewer", isPlatformAdmin: false }));
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Username"), "viewer");
    await user.type(screen.getByLabelText("Password"), "rahasia");
    await user.click(screen.getByRole("button", { name: "Login" }));

    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    expect(await screen.findByText("halaman rahasia")).toBeInTheDocument();
  });
});

describe("nextFor", () => {
  it("halaman admin cuma buat Platform Admin", () => {
    expect(nextFor("/admin/users", { isPlatformAdmin: false })).toBeNull();
    expect(nextFor("/admin/users", { isPlatformAdmin: true })).toBe("/admin/users");
    expect(nextFor("/databases", { isPlatformAdmin: false })).toBe("/databases");
    expect(nextFor(null, { isPlatformAdmin: true })).toBeNull();
  });
});

describe("safeNext", () => {
  it("cuma menerima path lokal", () => {
    expect(safeNext("/databases?page=2")).toBe("/databases?page=2");
    expect(safeNext("//evil.example")).toBeNull();
    expect(safeNext("https://evil.example")).toBeNull();
    expect(safeNext("/\\evil.example")).toBeNull();
    expect(safeNext("/login?next=/x")).toBeNull();
    expect(safeNext(null)).toBeNull();
  });
});