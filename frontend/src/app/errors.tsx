import { Link, isRouteErrorResponse, useRouteError } from "react-router";
import { PageHeader } from "../components/PageHeader";
import { EmptyState, ErrorState } from "../components/States";

export function NotFoundPage() {
  return (
    <>
      <PageHeader title="Halaman tidak ditemukan" />
      <EmptyState
        code="404"
        title="Alamat ini tidak ada"
        description="Mungkin link-nya salah atau halamannya sudah dipindah."
        actions={
          <Link to="/" className="btn btn-line btn-sm">
            Kembali ke beranda
          </Link>
        }
      />
    </>
  );
}

export function RouteError() {
  const err = useRouteError();
  const chunkFailed = err instanceof Error && /dynamically imported module|Loading chunk/i.test(err.message);
  if (isRouteErrorResponse(err) && err.status === 404) {
    return (
      <div className="bare">
        <NotFoundPage />
      </div>
    );
  }
  return (
    <div className="bare">
      <ErrorState
        title={chunkFailed ? "Versi aplikasi sudah diperbarui" : "Terjadi kesalahan di aplikasi"}
        description={chunkFailed ? "Muat ulang halaman untuk memakai versi terbaru." : "Coba muat ulang halaman. Kalau masih terjadi, laporkan ke tim."}
        onRetry={() => window.location.reload()}
      />
    </div>
  );
}