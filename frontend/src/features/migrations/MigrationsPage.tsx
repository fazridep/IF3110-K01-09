import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router";
import { api, type List } from "../../lib/api";
import { formatDateTime } from "../../lib/format";
import { DataTable } from "../../components/DataTable";
import { PageHeader } from "../../components/PageHeader";
import { Panel } from "../../components/Panel";
import { QueryState } from "../../components/QueryState";
import { SkeletonTable } from "../../components/Skeleton";
import { EmptyState } from "../../components/States";
import { StatusBadge } from "../../components/StatusBadge";
import type { DatabaseItem } from "../databases/types";
import { HistoryTable } from "./HistoryTable";
import { statusLabel, type Execution, type Migration } from "./types";

const POLL_MS = 3000;

export function useDatabase(dbId: string) {
  return useQuery({
    queryKey: ["databases", "list"],
    queryFn: () => api<List<DatabaseItem>>("/api/databases"),
    select: (d) => d.items.find((db) => db.id === dbId),
  });
}

export default function MigrationsPage() {
  const { dbId = "" } = useParams();
  const [hideRollback, setHideRollback] = useState(false);
  const db = useDatabase(dbId);
  const migrations = useQuery({
    queryKey: ["migrations", dbId],
    queryFn: () => api<List<Migration>>(`/api/databases/${dbId}/migrations`),
  });
  const history = useQuery({
    queryKey: ["migrations", dbId, "history", hideRollback],
    queryFn: () =>
      api<List<Execution>>(`/api/databases/${dbId}/history`, { query: { hideRollback: hideRollback || undefined } }),
    refetchInterval: (q) => (q.state.data?.items.some((e) => e.status === "running") ? POLL_MS : false),
  });

  const canEdit = db.data?.level === "view_edit";
  const hasDraft = migrations.data?.items.some((m) => m.status === "draft") ?? false;

  return (
    <div className="stack">
      <PageHeader
        title={db.data ? `Migration ${db.data.name}` : "Migration"}
        description={
          <>
            <Link to="/databases">Databases</Link> / <Link to={`/databases/${dbId}`}>{db.data?.name ?? "database"}</Link> / migration
          </>
        }
        actions={
          <>
            <Link className="btn btn-line" to={`/jobs?databaseId=${dbId}`}>Job</Link>
            {canEdit && !hasDraft && (
              <Link className="btn btn-accent" to={`/databases/${dbId}/migrations/new`}>Migration baru</Link>
            )}
          </>
        }
      />

      <Panel title="Daftar migration" flush>
        <QueryState
          query={migrations}
          skeleton={<SkeletonTable rows={4} columns={5} />}
          isEmpty={(d) => d.items.length === 0}
          empty={<EmptyState title="Belum ada migration" description={canEdit ? "Buat migration pertama lewat tombol Migration baru." : undefined} />}
        >
          {(d) => (
            <DataTable
              caption="Daftar migration"
              rows={d.items}
              rowKey={(m) => m.id}
              columns={[
                { header: "Versi", className: "nowrap", cell: (m) => <span className="chip">v{m.version}</span> },
                {
                  header: "Deskripsi",
                  cell: (m) => (
                    <Link to={`/migrations/${m.id}`}>
                      <strong>{m.description}</strong>
                    </Link>
                  ),
                },
                {
                  header: "Mode",
                  className: "nowrap",
                  cell: (m) => (
                    <>
                      <span className="chip">{m.mode === "batch" ? `batch ${m.batch?.size ?? ""}` : "normal"}</span>
                      {m.irreversible && <> <span className="chip">irreversible</span></>}
                    </>
                  ),
                },
                { header: "Status", className: "nowrap", cell: (m) => <StatusBadge status={m.status} label={statusLabel[m.status]} /> },
                { header: "Dibuat", className: "nowrap", cell: (m) => `${m.createdBy}, ${formatDateTime(m.createdAt)}` },
              ]}
            />
          )}
        </QueryState>
      </Panel>

      <Panel
        title="Riwayat eksekusi"
        flush
        actions={
          <label className="check">
            <input type="checkbox" checked={hideRollback} onChange={(e) => setHideRollback(e.target.checked)} />
            Hide rollback
          </label>
        }
      >
        <QueryState query={history} skeleton={<SkeletonTable rows={4} columns={7} />}>
          {(d) => <HistoryTable items={d.items} />}
        </QueryState>
      </Panel>
    </div>
  );
}
