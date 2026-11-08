import type { ReactNode } from "react";
import { Navigate, useLocation } from "react-router";
import { useAuth } from "./AuthContext";
import { ErrorState } from "../../components/States";
import { Skeleton } from "../../components/Skeleton";

export function RequireAuth({ children }: { children: ReactNode }) {
  const { user, loading } = useAuth();
  const location = useLocation();

  if (loading) {
    return (
      <div className="bare" aria-busy="true" aria-live="polite">
        <div style={{ width: "min(360px, 100%)" }}>
          <span className="visually-hidden">Memuat sesi…</span>
          <Skeleton lines={3} />
        </div>
      </div>
    );
  }
  if (!user) {
    const next = location.pathname + location.search;
    const to = next === "/" ? "/login" : `/login?next=${encodeURIComponent(next)}`;
    return <Navigate to={to} replace />;
  }
  return <>{children}</>;
}

export function RequireAdmin({ children }: { children: ReactNode }) {
  const { user } = useAuth();
  if (!user?.isPlatformAdmin) {
    return (
      <ErrorState
        title="Khusus Platform Admin"
        description="Halaman ini hanya untuk Platform Admin. Hubungi admin kalau kamu butuh akses."
        code="403"
      />
    );
  }
  return <>{children}</>;
}