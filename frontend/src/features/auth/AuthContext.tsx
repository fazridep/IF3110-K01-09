import { createContext, useCallback, useContext, useEffect, useMemo, type ReactNode } from "react";
import { useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { ApiError, api, setUnauthorizedHandler } from "../../lib/api";

export interface User {
  id: string;
  username: string;
  isPlatformAdmin: boolean;
}

interface AuthState {
  user: User | null;
  loading: boolean;
  login: (username: string, password: string) => Promise<User>;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthState | null>(null);

export const meQueryKey = ["me"] as const;

async function fetchMe(): Promise<User | null> {
  try {
    return await api<User>("/api/me", { skipAuthRedirect: true });
  } catch (err) {
    if (err instanceof ApiError && err.status === 401) return null;
    throw err;
  }
}

function clearUserData(qc: QueryClient) {
  qc.removeQueries({ predicate: (q) => q.queryKey[0] !== meQueryKey[0] });
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const qc = useQueryClient();
  const me = useQuery({ queryKey: meQueryKey, queryFn: fetchMe, staleTime: 60_000, retry: 1 });

  useEffect(() => {
    setUnauthorizedHandler(() => {
      qc.setQueryData(meQueryKey, null);
    });
    return () => setUnauthorizedHandler(undefined);
  }, [qc]);

  const login = useCallback(
    async (username: string, password: string) => {
      const user = await api<User>("/api/session", {
        method: "POST",
        body: { username, password },
        skipAuthRedirect: true,
      });
      clearUserData(qc);
      qc.setQueryData(meQueryKey, user);
      return user;
    },
    [qc],
  );

  const logout = useCallback(async () => {
    try {
      await api("/api/session", { method: "DELETE", skipAuthRedirect: true });
    } finally {
      clearUserData(qc);
      qc.setQueryData(meQueryKey, null);
    }
  }, [qc]);

  const value = useMemo<AuthState>(
    () => ({ user: me.data ?? null, loading: me.isPending, login, logout }),
    [me.data, me.isPending, login, logout],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth harus dipakai di dalam <AuthProvider>");
  return ctx;
}

export function useUser(): User {
  const { user } = useAuth();
  if (!user) throw new Error("useUser dipanggil di luar RequireAuth");
  return user;
}