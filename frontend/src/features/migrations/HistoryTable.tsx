import { Link } from "react-router";
import { formatDateTime, formatNumber } from "../../lib/format";
import { DataTable } from "../../components/DataTable";
import { EmptyState } from "../../components/States";
import { StatusBadge } from "../../components/StatusBadge";
import { actionLabel, shortId, type Execution } from "./types";

interface Props {
  items: Execution[];
  showMigration?: boolean;
}

// Riwayat eksekusi beserta semua attempt-nya. Attempt yang gagal tetap tampil.
export function HistoryTable({ items, showMigration = true }: Props) {
  return (
    <DataTable
      caption="Riwayat eksekusi migration"
      rows={items}
      rowKey={(e) => e.id}
      empty={<EmptyState title="Belum ada eksekusi" description="Eksekusi muncul di sini setelah job migration diambil worker." />}
      columns={[
        {
          header: "Eksekusi",
          className: "nowrap",
          cell: (e) => (
            <div style={{ display: "grid", gap: ".2rem" }}>
              <span className="chip" title={e.id}>E {shortId(e.id)}</span>
              {e.revertsExecutionId && (
                <span style={{ fontSize: ".75rem", color: "var(--muted)" }} title={e.revertsExecutionId}>
                  membatalkan E {shortId(e.revertsExecutionId)}
                </span>
              )}
            </div>
          ),
        },
        ...(showMigration
          ? [
              {
                header: "Migration",
                cell: (e: Execution) => (
                  <Link to={`/migrations/${e.migrationId}`}>
                    <strong>v{e.version}</strong> {e.description}
                  </Link>
                ),
              },
            ]
          : []),
        { header: "Aksi", className: "nowrap", cell: (e) => <span className="chip">{actionLabel[e.action] ?? e.action}</span> },
        {
          header: "Status",
          className: "nowrap",
          cell: (e) => <StatusBadge status={e.status} />,
        },
        {
          header: "Percobaan",
          cell: (e) =>
            e.attempts.length === 0 ? (
              <span style={{ color: "var(--muted)" }}>—</span>
            ) : (
              <ol style={{ listStyle: "none", margin: 0, padding: 0, display: "grid", gap: ".35rem" }}>
                {e.attempts.map((a) => (
                  <li key={`${a.jobId}-${a.attempt}-${a.status}`} style={{ display: "grid", gap: ".15rem" }}>
                    <span style={{ display: "flex", alignItems: "center", gap: ".35rem", flexWrap: "wrap" }}>
                      <span className="chip">#{a.attempt}</span>
                      <StatusBadge status={a.status} />
                      <span style={{ fontSize: ".75rem", color: "var(--muted)" }}>{formatDateTime(a.finishedAt)}</span>
                    </span>
                    {a.error && <span style={{ fontSize: ".75rem", color: "var(--muted)", overflowWrap: "anywhere" }}>{a.error}</span>}
                  </li>
                ))}
              </ol>
            ),
        },
        {
          header: "Batch / row",
          className: "nowrap",
          cell: (e) => (e.mode === "batch" ? `${formatNumber(e.batchesCommitted)} / ${formatNumber(e.rowsCommitted)}` : formatNumber(e.rowsCommitted)),
        },
        { header: "Oleh", className: "nowrap", cell: (e) => e.executedBy },
        { header: "Mulai", className: "nowrap", cell: (e) => formatDateTime(e.startedAt) },
        {
          header: "Job",
          className: "nowrap",
          cell: (e) => <Link to={`/jobs/${e.jobId}`}>{shortId(e.jobId)}</Link>,
        },
      ]}
    />
  );
}
