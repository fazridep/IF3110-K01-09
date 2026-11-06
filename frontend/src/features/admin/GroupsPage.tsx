import { useState, type FormEvent } from "react";
import { Link, useNavigate } from "react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, type List } from "../../lib/api";
import { Button } from "../../components/Button";
import { DataTable } from "../../components/DataTable";
import { ConfirmDialog, Dialog } from "../../components/Dialog";
import { Field } from "../../components/Field";
import { PageHeader } from "../../components/PageHeader";
import { Panel } from "../../components/Panel";
import { QueryState } from "../../components/QueryState";
import { SkeletonTable } from "../../components/Skeleton";
import { Alert, EmptyState } from "../../components/States";
import { StatusBadge } from "../../components/StatusBadge";
import type { AdminGroup } from "./types";

export default function GroupsPage() {
  const navigate = useNavigate();
  const qc = useQueryClient();

  const groups = useQuery({
    queryKey: ["groups"],
    queryFn: () => api<List<AdminGroup>>("/api/admin/groups"),
  });

  const [createOpen, setCreateOpen] = useState(false);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [deleteTarget, setDeleteTarget] = useState<AdminGroup | null>(null);
  const [deleteError, setDeleteError] = useState<unknown>(null);

  const createMutation = useMutation({
    mutationFn: (body: { name: string; description: string }) =>
      api<AdminGroup>("/api/admin/groups", { method: "POST", body }),
    onSuccess: async (created) => {
      setCreateOpen(false);
      setName("");
      setDescription("");
      await qc.invalidateQueries({ queryKey: ["groups"] });
      navigate(`/admin/groups/${created.id}`);
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) =>
      api<void>(`/api/admin/groups/${id}`, { method: "DELETE" }),
    onSuccess: async () => {
      setDeleteTarget(null);
      setDeleteError(null);
      await qc.invalidateQueries({ queryKey: ["groups"] });
    },
    onError: (err) => {
      setDeleteTarget(null);
      setDeleteError(err);
    },
  });

  function handleOpenCreate() {
    createMutation.reset();
    setName("");
    setDescription("");
    setCreateOpen(true);
  }

  function handleCreateSubmit(e: FormEvent) {
    e.preventDefault();
    createMutation.mutate({ name: name.trim(), description: description.trim() });
  }

  const createApiErr = createMutation.error instanceof ApiError ? createMutation.error : null;

  return (
    <div className="stack">
      <PageHeader
        title="Groups"
        description="Kelola group, tunjuk leader group, atur anggota, dan grant akses database."
        actions={
          <Button onClick={handleOpenCreate}>
            + Buat Group Baru
          </Button>
        }
      />

      {deleteError != null && <Alert tone="danger" error={deleteError} />}

      <Panel title="Daftar Group" flush>
        <QueryState
          query={groups}
          skeleton={<SkeletonTable rows={5} columns={5} />}
          isEmpty={(d) => d.items.length === 0}
          empty={
            <EmptyState
              title="Belum ada group"
              description="Buat group untuk mengelompokkan user, menunjuk leader, dan memberikan akses database."
            />
          }
        >
          {(d) => (
            <DataTable
              caption="Daftar group"
              rows={d.items}
              rowKey={(g) => g.id}
              columns={[
                {
                  header: "Nama",
                  cell: (g) => (
                    <Link
                      to={`/admin/groups/${g.id}`}
                      style={{ fontFamily: "var(--mono)", fontWeight: 600 }}
                    >
                      {g.name}
                    </Link>
                  ),
                },
                {
                  header: "Deskripsi",
                  cell: (g) => g.description || "—",
                },
                {
                  header: "Leader",
                  cell: (g) =>
                    g.leaderUsername ? (
                      <StatusBadge label={g.leaderUsername} tone="info" />
                    ) : (
                      <StatusBadge label="Belum ada leader" tone="warn" />
                    ),
                },
                {
                  header: "Anggota",
                  className: "nowrap",
                  cell: (g) => <span className="chip">{g.memberCount} anggota</span>,
                },
                {
                  header: "Aksi",
                  className: "nowrap",
                  cell: (g) => (
                    <div style={{ display: "flex", gap: ".5rem", flexWrap: "wrap" }}>
                      <Link to={`/admin/groups/${g.id}`} className="btn btn-line btn-sm">
                        Kelola
                      </Link>
                      <Button
                        variant="danger"
                        size="sm"
                        onClick={() => setDeleteTarget(g)}
                      >
                        Hapus
                      </Button>
                    </div>
                  ),
                },
              ]}
            />
          )}
        </QueryState>
      </Panel>

      <Dialog
        open={createOpen}
        onClose={() => !createMutation.isPending && setCreateOpen(false)}
        title="Buat Group Baru"
        footer={
          <>
            <Button
              variant="line"
              onClick={() => setCreateOpen(false)}
              disabled={createMutation.isPending}
            >
              Batal
            </Button>
            <Button
              type="submit"
              form="create-group-form"
              loading={createMutation.isPending}
            >
              Buat Group
            </Button>
          </>
        }
      >
        <form id="create-group-form" onSubmit={handleCreateSubmit} noValidate>
          {createMutation.isError && (
            <div style={{ marginBottom: "1rem" }}>
              <Alert tone="danger" error={createMutation.error} />
            </div>
          )}
          <Field label="Nama" error={createApiErr?.fieldError("name")}>
            <input
              className="input"
              name="name"
              placeholder="Contoh: backend-team"
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </Field>
          <Field label="Deskripsi" error={createApiErr?.fieldError("description")}>
            <input
              className="input"
              name="description"
              placeholder="Opsional"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </Field>
        </form>
      </Dialog>

      <ConfirmDialog
        open={deleteTarget != null}
        title="Hapus Group"
        confirmLabel="Hapus Group"
        danger
        loading={deleteMutation.isPending}
        onCancel={() => setDeleteTarget(null)}
        onConfirm={() => {
          if (deleteTarget) deleteMutation.mutate(deleteTarget.id);
        }}
      >
        <p>
          Apakah Anda yakin ingin menghapus group <strong>{deleteTarget?.name}</strong>? Semua anggota dan grant akses database pada group ini akan ikut dihapus.
        </p>
      </ConfirmDialog>
    </div>
  );
}

