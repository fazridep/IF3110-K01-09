import { useState } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router";
import { api, type List } from "../../lib/api";
import { formatDateTime } from "../../lib/format";
import { Button } from "../../components/Button";
import { DataTable } from "../../components/DataTable";
import { PageHeader } from "../../components/PageHeader";
import { Panel } from "../../components/Panel";
import { SkeletonTable } from "../../components/Skeleton";
import { EmptyState, ErrorState } from "../../components/States";
import { StatusBadge } from "../../components/StatusBadge";
import { isActive, type Job } from "./types";

const POLL_MS = 3000;

const FILTERS = [
  { value: "", label: "Semua" },
  { value: "queued", label: "Antre" },
  { value: "running", label: "Berjalan" },
  { value: "partial", label: "Partial" },
  { value: "uncertain", label: "Belum pasti" },
  { value: "failed", label: "Gagal" },
  { value: "success", label: "Sukses" },
];

export default function JobsPage() {
  const [params] = useSearchParams();
  const databaseId = params.get("databaseId") ?? undefined;
  const [status, setStatus] = useState("");

  const query = useInfiniteQuery({
    queryKey: ["jobs", databaseId, status],
    queryFn: ({ pageParam }) =>
      api<List<Job>>("/api/jobs", { query: { databaseId, status, cursor: pageParam } }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.nextCursor,
    refetchInterval: (q) => (q.state.data?.pages.some((p) => p.items.some((j) => isActive(j.status))) ? POLL_MS : false),
  });

  const jobs = query.data?.pages.flatMap((p) => p.items) ?? [];

  return (
    <div className="stack">
      <PageHeader title="Job & queue" description="Eksekusi migration berjalan di background. Halaman ini menampilkan statusnya." />
      <Panel
        title="Daftar Job"
        flush
        actions={
          <div style={{ display: "flex", gap: ".35rem", flexWrap: "wrap" }}>
            {FILTERS.map((f) => (
              <Button key={f.value} variant={status === f.value ? "accent" : "ghost"} size="sm" onClick={() => setStatus(f.value)}>
                {f.label}
              </Button>
            ))}
          </div>
        }
        footer={
          query.hasNextPage && (
            <Button variant="line" size="sm" loading={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()}>
              Muat lebih banyak
            </Button>
          )
        }
      >
        {query.isPending ? (
          <SkeletonTable rows={5} columns={6} />
        ) : query.isError ? (
          <ErrorState error={query.error} onRetry={() => void query.refetch()} />
        ) : jobs.length === 0 ? (
          <EmptyState title="Belum ada job" description="Job muncul setelah perubahan yang sudah disetujui dijalankan dari halaman Approval." />
        ) : (
          <DataTable
            caption="Daftar job migration"
            rows={jobs}
            rowKey={(j) => j.id}
            columns={[
              {
                header: "Migration",
                cell: (j) => (
                  <div style={{ display: "flex", alignItems: "center", gap: ".45rem", flexWrap: "wrap" }}>
                    <Link to={`/jobs/${j.id}`}><strong style={{ fontFamily: "var(--mono)" }}>{j.databaseName}</strong></Link>
                    <span className="chip">v{j.migrationVersion}</span>
                    <span className="chip">{j.action}</span>
                  </div>
                ),
              },
              { header: "Deskripsi", cell: (j) => j.description },
              { header: "Status", className: "nowrap", cell: (j) => <StatusBadge status={j.status} /> },
              { header: "Percobaan", className: "nowrap", cell: (j) => `${j.attempt}/${j.maxAttempts}` },
              { header: "Oleh", cell: (j) => j.createdBy },
              { header: "Dibuat", className: "nowrap", cell: (j) => formatDateTime(j.createdAt) },
            ]}
          />
        )}
      </Panel>
    </div>
  );
}
