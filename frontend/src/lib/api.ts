
export interface Problem {
  type: string;
  title: string;
  status: number;
  detail?: string;
  instance?: string;
  errors?: { field: string; detail: string }[];
  requestId?: string;
}

export interface List<T> {
  items: T[];
  nextCursor?: string;
}

export interface JobAccepted {
  jobId: string;
  status: string;
  statusUrl: string;
}

export class ApiError extends Error {
  readonly status: number;
  readonly problem: Problem;

  constructor(problem: Problem) {
    super(problem.detail || problem.title);
    this.name = "ApiError";
    this.status = problem.status;
    this.problem = problem;
  }

  get code(): string {
    return this.problem.type.replace(/^urn:problem:/, "");
  }

  fieldError(field: string): string | undefined {
    return this.problem.errors?.find((e) => e.field === field)?.detail;
  }
}

const CSRF_COOKIE = "csrf";
const SAFE_METHODS = new Set(["GET", "HEAD", "OPTIONS"]);

export function readCookie(name: string): string | undefined {
  for (const part of document.cookie.split(";")) {
    const [k, ...v] = part.trim().split("=");
    if (k === name) return decodeURIComponent(v.join("="));
  }
  return undefined;
}

async function ensureCsrfToken(): Promise<string> {
  let token = readCookie(CSRF_COOKIE);
  if (!token) {
    await fetch("/api/health", { credentials: "same-origin" });
    token = readCookie(CSRF_COOKIE);
  }
  return token ?? "";
}

type UnauthorizedHandler = () => void;
let onUnauthorized: UnauthorizedHandler | undefined;

export function setUnauthorizedHandler(fn: UnauthorizedHandler | undefined) {
  onUnauthorized = fn;
}

export interface RequestOptions {
  method?: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
  body?: unknown;
  query?: Record<string, string | number | boolean | undefined>;
  signal?: AbortSignal;
  skipAuthRedirect?: boolean;
}

function buildUrl(path: string, query?: RequestOptions["query"]): string {
  if (!query) return path;
  const params = new URLSearchParams();
  for (const [k, v] of Object.entries(query)) {
    if (v !== undefined && v !== "") params.set(k, String(v));
  }
  const qs = params.toString();
  return qs ? `${path}?${qs}` : path;
}

async function toProblem(res: Response): Promise<Problem> {
  const fallback: Problem = {
    type: "about:blank",
    title: res.statusText || "Request gagal",
    status: res.status,
    requestId: res.headers.get("X-Request-ID") ?? undefined,
  };
  const ct = res.headers.get("Content-Type") ?? "";
  if (!ct.includes("json")) return fallback;
  try {
    const body = (await res.json()) as Partial<Problem>;
    return { ...fallback, ...body, status: body.status ?? res.status };
  } catch {
    return fallback;
  }
}

export async function api<T = unknown>(path: string, opts: RequestOptions = {}): Promise<T> {
  const method = opts.method ?? "GET";
  const headers: Record<string, string> = { Accept: "application/json" };
  if (opts.body !== undefined) headers["Content-Type"] = "application/json";
  if (!SAFE_METHODS.has(method)) headers["X-CSRF-Token"] = await ensureCsrfToken();

  const res = await fetch(buildUrl(path, opts.query), {
    method,
    headers,
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
    credentials: "same-origin",
    signal: opts.signal,
  });

  if (!res.ok) {
    const problem = await toProblem(res);
    if (res.status === 401 && !opts.skipAuthRedirect) onUnauthorized?.();
    throw new ApiError(problem);
  }
  if (res.status === 204 || res.headers.get("Content-Length") === "0") {
    return undefined as T;
  }
  return (await res.json()) as T;
}

export function errorMessage(err: unknown): string {
  if (err instanceof ApiError) return err.message;
  if (err instanceof TypeError) return "Tidak bisa terhubung ke server. Periksa koneksi lalu coba lagi.";
  if (err instanceof Error) return err.message;
  return "Terjadi kesalahan.";
}