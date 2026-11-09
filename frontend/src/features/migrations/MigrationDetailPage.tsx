import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "react-router";
import { api, type JobAccepted, type List } from "../../lib/api";
import { formatDateTime } from "../../lib/format";
import { Button } from "../../components/Button";
import { CodeBlock } from "../../components/CodeBlock";
import { DataTable } from "../../components/DataTable";
import { PageHeader } from "../../components/PageHeader";
import { Panel } from "../../components/Panel";
import { QueryState } from "../../components/QueryState";
import { Skeleton } from "../../components/Skeleton";
import { Alert, EmptyState } from "../../components/States";
import { StatusBadge } from "../../components/StatusBadge";
import type { ApprovalItem } from "../approvals/types";
import { HistoryTable } from "./HistoryTable";
import { MigrationForm } from "./MigrationForm";
import { actionLabel, statusLabel, type Execution, type MigrationAction, type MigrationDetail } from "./types";

const POLL_MS = 3000;

function isRunning(list: List<Execution> | undefined): boolean {
  return list?.items.some((e) => e.status === "running") ?? false;
}

function explain(m: MigrationDetail): { tone: "warn" | "danger" | "info"; text: string } | null {
  switch (m.status) {
    case "draft":
      if (m.error) {
        return { tone: "warn", text: "Percobaan terakhir gagal sebelum SQL sempat dijalankan, jadi migration ini kembali ke draft. Approval lama sudah terpakai, ajukan Apply lagi." };
      }
      return { tone: "info", text: "Draft masih bisa diubah. Ajukan approval Apply ke leader group, lalu jalankan setelah semua setuju." };
    case "partial":
      return { tone: "warn", text: "Sebagian batch sudah committed. Ajukan Lanjutkan batch untuk meneruskan dari checkpoint, atau Rollback untuk membatalkan batch yang sudah committed. Migration lain di database ini ditahan." };
    case "partially_reverted":
      return { tone: "warn", text: "Pembatalan batch berhenti di tengah. Ajukan Rollback lagi untuk melanjutkan dari checkpoint." };
    case "failed":
      return { tone: "danger", text: "Eksekusi gagal dan sudah di-rollback. SQL migration nggak bisa diubah, buat migration baru untuk perbaikan." };
    case "rolled_back":
      return { tone: "info", text: "Perubahan migration ini sudah dibatalkan. Forward menjalankan SQL up yang sama lagi." };
    default:
      return null;
  }
}

export default function MigrationDetailPage() {
  const { id = "" } = useParams();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [editing, setEditing] = useState(false);

  const historyKey = ["migrations", "detail", id, "history"];
  const detail = useQuery({
    queryKey: ["migrations", "detail", id],
    queryFn: () => api<MigrationDetail>(`/api/migrations/${id}`),
    refetchInterval: () => (isRunning(qc.getQueryData<List<Execution>>(historyKey)) ? POLL_MS : false),
  });
  const dbId = detail.data?.databaseId;
  const history = useQuery({
    queryKey: historyKey,
    enabled: dbId != null,
    queryFn: () => api<List<Execution>>(`/api/databases/${dbId}/history`, { query: { migrationId: id } }),
    refetchInterval: (q) => (isRunning(q.state.data) ? POLL_MS : false),
  });

  const approvals = useQuery({
    queryKey: ["approvals", "migration", id],
    enabled: dbId != null,
    queryFn: () => api<List<ApprovalItem>>("/api/approvals", { query: { databaseId: dbId } }),
    select: (d) => d.items.filter((a) => a.migrationId === id),
  });

  const refresh = async () => {
    await Promise.all([
      qc.invalidateQueries({ queryKey: ["migrations", "detail", id] }),
      qc.invalidateQueries({ queryKey: ["approvals"] }),
    ]);
  };

  const update = useMutation({
    mutationFn: (body: unknown) => api(`/api/migrations/${id}`, { method: "PUT", body }),
    onSuccess: async () => {
      setEditing(false);
      await refresh();
      if (dbId) await qc.invalidateQueries({ queryKey: ["migrations", dbId] });
    },
  });
  const submit = useMutation({
    mutationFn: (action: MigrationAction) => api<ApprovalItem>("/api/approvals", { method: "POST", body: { migrationId: id, action } }),
    onSuccess: refresh,
  });
  // Aman diklik ulang: satu approval cuma jadi satu job.
  const run = useMutation({
    mutationFn: (approvalId: string) => api<JobAccepted>("/api/jobs", { method: "POST", body: { approvalId } }),
    onSuccess: (job) => navigate(`/jobs/${job.jobId}`),
  });

  return (
    <QueryState query={detail} skeleton={<Skeleton lines={8} />}>
      {(m) => {
        const note = explain(m);
        const pending = approvals.data?.some((a) => a.status === "pending_review") ?? false;
        return (
          <div className="stack">
            <PageHeader
              title={`v${m.version} ${m.description}`}
              description={
                <>
                  <Link to={`/databases/${m.databaseId}/migrations`}>Migration</Link> /{" "}
                  <StatusBadge status={m.status} label={statusLabel[m.status]} />
                </>
              }
              actions={
                m.canEdit &&
                m.status === "draft" &&
                !editing && (
                  <Button variant="line" onClick={() => setEditing(true)}>
                    Ubah draft
                  </Button>
                )
              }
            />
            {note && <Alert tone={note.tone}>{note.text}</Alert>}
            {m.error && m.status !== "success" && <Alert tone="danger" title="Pesan terakhir">{m.error}</Alert>}

            {editing ? (
              <Panel title="Ubah draft">
                <Alert tone="info">Approval yang belum dijalankan untuk draft ini akan dibatalkan, karena yang disetujui leader adalah SQL lama.</Alert>
                <div style={{ marginTop: "1rem" }}>
                  <MigrationForm
                    initial={m}
                    submitLabel="Simpan perubahan"
                    saving={update.isPending}
                    error={update.error}
                    onSubmit={(input) => update.mutate(input)}
                    onCancel={() => setEditing(false)}
                  />
                </div>
              </Panel>
            ) : (
              <Panel title="Definisi">
                <div className="stack" style={{ gap: "1rem" }}>
                  <dl className="kv">
                    <dt>Versi</dt>
                    <dd><span className="chip">v{m.version}</span></dd>
                    <dt>Mode</dt>
                    <dd>
                      {m.mode === "batch" && m.batch
                        ? `Per batch: ${m.batch.size} row dari ${m.batch.table}, kursor ${m.batch.key}${m.batch.backup ? ", data pemulihan disimpan" : ""}`
                        : "Normal, satu transaction"}
                    </dd>
                    <dt>Rollback</dt>
                    <dd>{m.irreversible ? `Irreversible: ${m.irreversibleReason}` : "Pakai SQL down"}</dd>
                    <dt>Dibuat</dt>
                    <dd>{m.createdBy}, {formatDateTime(m.createdAt)}</dd>
                    {m.executedBy && (
                      <>
                        <dt>Pertama dijalankan</dt>
                        <dd>{m.executedBy}, {formatDateTime(m.startedAt)}</dd>
                      </>
                    )}
                    <dt>Migration ID</dt>
                    <dd><span className="chip">{m.id}</span></dd>
                  </dl>
                  <div>
                    <h3 style={{ fontSize: ".95rem", margin: "0 0 .4rem" }}>SQL up</h3>
                    <CodeBlock code={m.upSql} label="SQL up" />
                  </div>
                  {!m.irreversible && (
                    <div>
                      <h3 style={{ fontSize: ".95rem", margin: "0 0 .4rem" }}>SQL down</h3>
                      <CodeBlock code={m.downSql} label="SQL down" />
                    </div>
                  )}
                </div>
              </Panel>
            )}

            <Panel title="Approval & eksekusi">
              <div className="stack" style={{ gap: "1rem" }}>
                {submit.isError && <Alert tone="danger" error={submit.error} />}
                {run.isError && <Alert tone="danger" error={run.error} />}
                {m.canEdit ? (
                  m.allowedActions.length > 0 ? (
                    <div style={{ display: "flex", gap: ".5rem", flexWrap: "wrap", alignItems: "center" }}>
                      {m.allowedActions.map((a) => (
                        <Button
                          key={a}
                          variant={a === "rollback" ? "danger" : "accent"}
                          disabled={pending || editing}
                          loading={submit.isPending && submit.variables === a}
                          onClick={() => submit.mutate(a)}
                        >
                          Ajukan {actionLabel[a]}
                        </Button>
                      ))}
                      {pending && <span style={{ color: "var(--muted)", fontSize: ".85rem" }}>Masih ada approval yang menunggu review.</span>}
                    </div>
                  ) : (
                    <p style={{ margin: 0, color: "var(--muted)" }}>Belum ada aksi yang bisa diajukan untuk posisi migration ini.</p>
                  )
                ) : (
                  <p style={{ margin: 0, color: "var(--muted)" }}>Akses kamu View Only, jadi cuma bisa melihat.</p>
                )}

                <QueryState
                  query={approvals}
                  skeleton={<Skeleton lines={2} />}
                  isEmpty={(d) => d.length === 0}
                  empty={<EmptyState title="Belum ada approval" />}
                >
                  {(items) => (
                    <DataTable
                      caption="Approval migration ini"
                      rows={items}
                      rowKey={(a) => a.id}
                      columns={[
                        { header: "Aksi", className: "nowrap", cell: (a) => <span className="chip">{actionLabel[a.action] ?? a.action}</span> },
                        { header: "Status", className: "nowrap", cell: (a) => <StatusBadge status={a.status} /> },
                        { header: "Leader", className: "nowrap", cell: (a) => `${a.approvedCount}/${a.totalRequired} group setuju` },
                        { header: "Diajukan", className: "nowrap", cell: (a) => `${a.requestedBy}, ${formatDateTime(a.createdAt)}` },
                        {
                          header: "",
                          className: "nowrap",
                          cell: (a) =>
                            a.status === "approved" && m.canEdit ? (
                              <Button size="sm" loading={run.isPending && run.variables === a.id} onClick={() => run.mutate(a.id)}>
                                Jalankan
                              </Button>
                            ) : a.status === "pending_review" ? (
                              <Link to="/approvals">Lihat di inbox</Link>
                            ) : null,
                        },
                      ]}
                    />
                  )}
                </QueryState>
              </div>
            </Panel>

            <Panel title="Riwayat eksekusi migration ini" flush>
              <QueryState query={history} skeleton={<Skeleton lines={3} />}>
                {(d) => <HistoryTable items={d.items} showMigration={false} />}
              </QueryState>
            </Panel>
          </div>
        );
      }}
    </QueryState>
  );
}
