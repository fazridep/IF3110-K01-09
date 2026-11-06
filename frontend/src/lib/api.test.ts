import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, api, errorMessage, setUnauthorizedHandler } from "./api";

function jsonResponse(status: number, body: unknown, contentType = "application/json") {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
    headers: { "Content-Type": contentType, "X-Request-ID": "req-9" },
  });
}

describe("api()", () => {
  const fetchMock = vi.fn<typeof fetch>();

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock);
    fetchMock.mockReset();
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    setUnauthorizedHandler(undefined);
  });

  it("GET mengembalikan JSON dan memakai cookie same-origin, tanpa header CSRF", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(200, { items: [1, 2] }));
    const out = await api<{ items: number[] }>("/api/things", { query: { q: "a b", empty: "", page: 2 } });

    expect(out.items).toEqual([1, 2]);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/things?q=a+b&page=2");
    expect(init?.credentials).toBe("same-origin");
    expect((init?.headers as Record<string, string>)["X-CSRF-Token"]).toBeUndefined();
  });

  it("POST mengirim body JSON dan token CSRF dari cookie", async () => {
    document.cookie = "csrf=tok123; path=/";
    fetchMock.mockResolvedValueOnce(jsonResponse(201, { id: "g1" }));
    await api("/api/groups", { method: "POST", body: { name: "tim" } });

    const [, init] = fetchMock.mock.calls[0];
    const headers = init?.headers as Record<string, string>;
    expect(headers["X-CSRF-Token"]).toBe("tok123");
    expect(headers["Content-Type"]).toBe("application/json");
    expect(init?.body).toBe(JSON.stringify({ name: "tim" }));
  });

  it("minta cookie CSRF dulu kalau belum ada", async () => {
    fetchMock.mockImplementationOnce(async () => {
      document.cookie = "csrf=baru; path=/";
      return jsonResponse(200, { status: "ok" });
    });
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 204 }));

    const out = await api("/api/session", { method: "DELETE" });
    expect(out).toBeUndefined();
    expect(fetchMock.mock.calls[0][0]).toBe("/api/health");
    expect((fetchMock.mock.calls[1][1]?.headers as Record<string, string>)["X-CSRF-Token"]).toBe("baru");
  });

  it("problem+json jadi ApiError lengkap dengan field error dan requestId", async () => {
    document.cookie = "csrf=tok123; path=/";
    fetchMock.mockResolvedValueOnce(
      jsonResponse(
        400,
        {
          type: "urn:problem:invalid-input",
          title: "Input tidak valid",
          status: 400,
          detail: "Nama wajib diisi.",
          errors: [{ field: "name", detail: "wajib diisi" }],
          requestId: "req-1",
        },
        "application/problem+json",
      ),
    );

    const err = await api("/api/groups", { method: "POST", body: {} }).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    const apiErr = err as ApiError;
    expect(apiErr.status).toBe(400);
    expect(apiErr.code).toBe("invalid-input");
    expect(apiErr.message).toBe("Nama wajib diisi.");
    expect(apiErr.fieldError("name")).toBe("wajib diisi");
    expect(apiErr.problem.requestId).toBe("req-1");
  });

  it("response error non-JSON tetap jadi ApiError", async () => {
    fetchMock.mockResolvedValueOnce(new Response("bad gateway", { status: 502, statusText: "Bad Gateway", headers: { "X-Request-ID": "r2" } }));
    const err = (await api("/api/x").catch((e: unknown) => e)) as ApiError;
    expect(err.status).toBe(502);
    expect(err.problem.requestId).toBe("r2");
  });

  it("401 memanggil handler global kecuali skipAuthRedirect", async () => {
    const onUnauthorized = vi.fn();
    setUnauthorizedHandler(onUnauthorized);
    const unauthorized = () => jsonResponse(401, { type: "urn:problem:unauthenticated", title: "Perlu login", status: 401 }, "application/problem+json");

    fetchMock.mockResolvedValueOnce(unauthorized());
    await expect(api("/api/databases")).rejects.toBeInstanceOf(ApiError);
    expect(onUnauthorized).toHaveBeenCalledTimes(1);

    fetchMock.mockResolvedValueOnce(unauthorized());
    await expect(api("/api/me", { skipAuthRedirect: true })).rejects.toBeInstanceOf(ApiError);
    expect(onUnauthorized).toHaveBeenCalledTimes(1);
  });

  it("errorMessage ramah untuk jaringan putus", () => {
    expect(errorMessage(new TypeError("Failed to fetch"))).toMatch(/terhubung ke server/);
  });
});