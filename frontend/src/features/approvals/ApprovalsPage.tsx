import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router";
import { api, ApiError, type JobAccepted, type List } from "../../lib/api";
import { formatDateTime } from "../../lib/format";
import { Button } from "../../components/Button";
import { CodeBlock } from "../../components/CodeBlock";
import { DataTable } from "../../components/DataTable";
import { Dialog } from "../../components/Dialog";
import { Field } from "../../components/Field";
import { PageHeader } from "../../components/PageHeader";
import { Panel } from "../../components/Panel";
import { QueryState } from "../../components/QueryState";
import { SkeletonTable } from "../../components/Skeleton";
import { Alert, EmptyState } from "../../components/States";
import { StatusBadge } from "../../components/StatusBadge";
import type { ApprovalItem } from "./types";

const STATUS_FILTERS: { value: string; label: string }[] = [
  { value: "", label: "Semua" },
  { value: "pending_review", label: "Pending Review" },
  { value: "approved", label: "Approved" },
  { value: "rejected", label: "Ditolak" },
];

export default function ApprovalsPage() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [statusFilter, setStatusFilter] = useState<string>("");
  const [reviewItem, setReviewItem] = useState<ApprovalItem | null>(null);
  const [approveTarget, setApproveTarget] = useState<ApprovalItem | null>(null);
  const [approveNote, setApproveNote] = useState("");
  const [rejectTarget, setRejectTarget] = useState<ApprovalItem | null>(null);
  const [rejectReason, setRejectReason] = useState("");
  const [actionError, setActionError] = useState<unknown>(null);

  const approvalsQuery = useQuery({
    queryKey: ["approvals", statusFilter],
    queryFn: () =>
      api<List<ApprovalItem>>("/api/approvals", {
        query: statusFilter ? { status: statusFilter } : undefined,
      }),
  });

  const approveMutation = useMutation({
    mutationFn: (vars: { id: string; note: string }) =>
      api<ApprovalItem>(`/api/approvals/${vars.id}/approve`, {
        method: "POST",
        body: { note: vars.note },
      }),
    onSuccess: async (updated) => {
      setApproveTarget(null);
      setApproveNote("");
      setActionError(null);
      if (reviewItem?.id === updated.id) {
        setReviewItem(updated);
      }
      await qc.invalidateQueries({ queryKey: ["approvals"] });
    },
    onError: (err) => setActionError(err),
  });

  const rejectMutation = useMutation({
    mutationFn: (vars: { id: string; reason: string }) =>
      api<ApprovalItem>(`/api/approvals/${vars.id}/reject`, {
        method: "POST",
        body: { reason: vars.reason },
      }),
    onSuccess: async (updated) => {
      setRejectTarget(null);
      setRejectReason("");
      setActionError(null);
      if (reviewItem?.id === updated.id) {
        setReviewItem(updated);
      }
      await qc.invalidateQueries({ queryKey: ["approvals"] });
    },
  });

  function openApproveDialog(item: ApprovalItem) {
    approveMutation.reset();
    setApproveNote("");
    setApproveTarget(item);
  }

  function openRejectDialog(item: ApprovalItem) {
    rejectMutation.reset();
    setRejectReason("");
    setRejectTarget(item);
  }

  // Aman diklik ulang: satu approval cuma jadi satu job, jadi yang kedua kalinya
  // cuma membuka job yang sama.
  const runMutation = useMutation({
    mutationFn: (id: string) =>
      api<JobAccepted>("/api/jobs", { method: "POST", body: { approvalId: id } }),
    onSuccess: (job) => navigate(`/jobs/${job.jobId}`),
    onError: (err) => setActionError(err),
  });

  function handleApproveSubmit(e: FormEvent) {
    e.preventDefault();
    if (!approveTarget) return;
    approveMutation.mutate({ id: approveTarget.id, note: approveNote.trim() });
  }

  function handleRejectSubmit(e: FormEvent) {
    e.preventDefault();
    if (!rejectTarget) return;
    rejectMutation.mutate({ id: rejectTarget.id, reason: rejectReason.trim() });
  }

  const rejectApiErr = rejectMutation.error instanceof ApiError ? rejectMutation.error : null;

  return (
    <div className="stack">
      <PageHeader
        title="Approval"
        description="Inbox persetujuan perubahan database. Setiap perubahan wajib disetujui oleh Leader dari semua group yang memiliki akses ke database terkait."
      />

      {actionError != null && <Alert tone="danger" error={actionError} />}

      <Panel
        title="Inbox Approval"
        flush
        actions={
          <div style={{ display: "flex", gap: ".35rem", flexWrap: "wrap" }}>
            {STATUS_FILTERS.map((f) => (
              <Button
                key={f.value}
                variant={statusFilter === f.value ? "accent" : "ghost"}
                size="sm"
                onClick={() => setStatusFilter(f.value)}
              >
                {f.label}
              </Button>
            ))}
          </div>
        }
      >
        <QueryState
          query={approvalsQuery}
          skeleton={<SkeletonTable rows={5} columns={6} />}
          isEmpty={(d) => d.items.length === 0}
          empty={
            <EmptyState
              title="Belum ada permintaan approval"
              description="Permintaan perubahan database yang membutuhkan persetujuan leader group akan muncul di sini."
            />
          }
        >
          {(d) => (
            <DataTable
              caption="Daftar permintaan approval"
              rows={d.items}
              rowKey={(item) => item.id}
              columns={[
                {
                  header: "Database & Versi",
                  cell: (item) => (
                    <div style={{ display: "flex", alignItems: "center", gap: ".45rem", flexWrap: "wrap" }}>
                      <strong style={{ fontFamily: "var(--mono)" }}>{item.databaseName}</strong>
                      <span className="chip">v{item.version}</span>
                      <span className="chip">{item.action}</span>
                    </div>
                  ),
                },
                {
                  header: "Deskripsi & Pengaju",
                  cell: (item) => (
                    <div>
                      <div>{item.description}</div>
                      <div style={{ fontSize: ".8rem", color: "var(--muted)" }}>
                        Oleh <strong style={{ fontFamily: "var(--mono)" }}>{item.requestedBy}</strong>
                      </div>
                    </div>
                  ),
                },
                {
                  header: "Quorum Leader",
                  className: "nowrap",
                  cell: (item) => (
                    <div style={{ display: "flex", flexDirection: "column", gap: ".25rem" }}>
                      <span className="chip">
                        {item.approvedCount}/{item.totalRequired} group setuju
                      </span>
                      {item.myLeaderGroups.length > 0 && (
                        <span style={{ fontSize: ".75rem", color: "var(--muted)", fontFamily: "var(--mono)" }}>
                          Leader: {item.myLeaderGroups.join(", ")}
                        </span>
                      )}
                    </div>
                  ),
                },
                {
                  header: "Status",
                  className: "nowrap",
                  cell: (item) => <StatusBadge status={item.status} />,
                },
                {
                  header: "Diajukan",
                  className: "nowrap",
                  cell: (item) => formatDateTime(item.createdAt),
                },
                {
                  header: "Aksi",
                  className: "nowrap",
                  cell: (item) => (
                    <div style={{ display: "flex", gap: ".45rem", flexWrap: "wrap" }}>
                      <Button variant="line" size="sm" onClick={() => setReviewItem(item)}>
                        Tinjau SQL
                      </Button>
                      {item.status === "approved" && (
                        <Button
                          variant="accent"
                          size="sm"
                          loading={runMutation.isPending && runMutation.variables === item.id}
                          onClick={() => runMutation.mutate(item.id)}
                        >
                          Jalankan
                        </Button>
                      )}
                      {item.canDecide && (
                        <>
                          <Button
                            variant="accent"
                            size="sm"
                            onClick={() => openApproveDialog(item)}
                          >
                            Setujui
                          </Button>
                          <Button
                            variant="danger"
                            size="sm"
                            onClick={() => openRejectDialog(item)}
                          >
                            Tolak
                          </Button>
                        </>
                      )}
                    </div>
                  ),
                },
              ]}
            />
          )}
        </QueryState>
      </Panel>

      <Dialog
        open={reviewItem != null}
        onClose={() => setReviewItem(null)}
        title={
          reviewItem
            ? `Review Migration v${reviewItem.version} · ${reviewItem.databaseName}`
            : "Review Migration"
        }
        footer={
          <>
            <Button variant="line" onClick={() => setReviewItem(null)}>
              Tutup
            </Button>
            {reviewItem?.canDecide && (
              <>
                <Button variant="danger" onClick={() => openRejectDialog(reviewItem)}>
                  Tolak
                </Button>
                <Button variant="accent" onClick={() => openApproveDialog(reviewItem)}>
                  Setujui
                </Button>
              </>
            )}
          </>
        }
      >
        {reviewItem && (
          <div className="stack">
            <dl className="kv">
              <dt>Database</dt>
              <dd style={{ fontFamily: "var(--mono)", fontWeight: 600 }}>{reviewItem.databaseName}</dd>
              <dt>Versi & Aksi</dt>
              <dd>
                <span className="chip">v{reviewItem.version}</span>{" "}
                <span className="chip">{reviewItem.action}</span>
              </dd>
              <dt>Deskripsi</dt>
              <dd>{reviewItem.description}</dd>
              <dt>Diajukan Oleh</dt>
              <dd style={{ fontFamily: "var(--mono)" }}>{reviewItem.requestedBy}</dd>
              <dt>Status</dt>
              <dd>
                <StatusBadge status={reviewItem.status} /> ({reviewItem.approvedCount}/{reviewItem.totalRequired} group setuju)
              </dd>
              {reviewItem.rejectionReason && (
                <>
                  <dt>Alasan Ditolak</dt>
                  <dd style={{ color: "var(--danger)" }}>{reviewItem.rejectionReason}</dd>
                </>
              )}
            </dl>

            <Panel title="SQL Migration" flush>
              <CodeBlock code={reviewItem.sql} />
            </Panel>

            <Panel title="Persetujuan Leader per Group" flush>
              <DataTable
                caption="Status persetujuan tiap group"
                rows={reviewItem.approvals}
                rowKey={(gd) => gd.groupId}
                columns={[
                  {
                    header: "Group",
                    cell: (gd) => <strong style={{ fontFamily: "var(--mono)" }}>{gd.groupName}</strong>,
                  },
                  {
                    header: "Hak Akses",
                    cell: (gd) => (
                      <span className="chip">
                        {gd.accessLevel === "view_edit" ? "View + Edit" : "View Only"}
                      </span>
                    ),
                  },
                  {
                    header: "Leader Group",
                    cell: (gd) =>
                      gd.leaderUsername ? (
                        <span style={{ fontFamily: "var(--mono)" }}>{gd.leaderUsername}</span>
                      ) : (
                        <StatusBadge label="Belum ada leader" tone="warn" />
                      ),
                  },
                  {
                    header: "Keputusan",
                    cell: (gd) => <StatusBadge status={gd.decision} />,
                  },
                  {
                    header: "Catatan",
                    cell: (gd) => gd.note || "—",
                  },
                ]}
              />
            </Panel>
          </div>
        )}
      </Dialog>

      <Dialog
        open={approveTarget != null}
        onClose={() => !approveMutation.isPending && setApproveTarget(null)}
        title="Setujui Perubahan Database"
        footer={
          <>
            <Button
              variant="line"
              onClick={() => setApproveTarget(null)}
              disabled={approveMutation.isPending}
            >
              Batal
            </Button>
            <Button
              type="submit"
              form="approve-form"
              loading={approveMutation.isPending}
            >
              Konfirmasi Setuju
            </Button>
          </>
        }
      >
        <form id="approve-form" onSubmit={handleApproveSubmit} noValidate>
          {approveMutation.isError && (
            <div style={{ marginBottom: "1rem" }}>
              <Alert tone="danger" error={approveMutation.error} />
            </div>
          )}
          <p style={{ marginTop: 0 }}>
            Anda menyetujui migration <strong>v{approveTarget?.version}</strong> pada database{" "}
            <strong>{approveTarget?.databaseName}</strong> mewakili group:{" "}
            <strong>{approveTarget?.myLeaderGroups.join(", ")}</strong>.
          </p>
          <Field label="Catatan (Opsional)">
            <input
              className="input"
              name="note"
              placeholder="Contoh: Sudah dicek aman untuk dijalankan"
              value={approveNote}
              onChange={(e) => setApproveNote(e.target.value)}
            />
          </Field>
        </form>
      </Dialog>

      <Dialog
        open={rejectTarget != null}
        onClose={() => !rejectMutation.isPending && setRejectTarget(null)}
        title="Tolak Perubahan Database"
        footer={
          <>
            <Button
              variant="line"
              onClick={() => setRejectTarget(null)}
              disabled={rejectMutation.isPending}
            >
              Batal
            </Button>
            <Button
              type="submit"
              form="reject-form"
              variant="danger"
              loading={rejectMutation.isPending}
            >
              Tolak Permintaan
            </Button>
          </>
        }
      >
        <form id="reject-form" onSubmit={handleRejectSubmit} noValidate>
          {rejectMutation.isError && (
            <div style={{ marginBottom: "1rem" }}>
              <Alert tone="danger" error={rejectMutation.error} />
            </div>
          )}
          <p style={{ marginTop: 0 }}>
            Menolak migration <strong>v{rejectTarget?.version}</strong> pada database{" "}
            <strong>{rejectTarget?.databaseName}</strong> akan langsung membatalkan permintaan ini.
          </p>
          <Field label="Alasan Penolakan" error={rejectApiErr?.fieldError("reason")}>
            <textarea
              className="textarea"
              name="reason"
              placeholder="Jelaskan alasan penolakan agar pengaju dapat memperbaiki SQL migration..."
              required
              value={rejectReason}
              onChange={(e) => setRejectReason(e.target.value)}
            />
          </Field>
        </form>
      </Dialog>
    </div>
  );
}

