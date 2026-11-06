import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useUser } from "../../auth/AuthContext";
import { api, ApiError, type List } from "../../lib/api";
import { formatDateTime } from "../../lib/format";
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
import type { AdminUser } from "./types";

export default function UsersPage() {
  const currentUser = useUser();
  const qc = useQueryClient();

  const usersQuery = useQuery({
    queryKey: ["admin", "users"],
    queryFn: () => api<List<AdminUser>>("/api/admin/users"),
  });

  const [createOpen, setCreateOpen] = useState(false);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [actionError, setActionError] = useState<unknown>(null);
  const [confirmTarget, setConfirmTarget] = useState<{
    user: AdminUser;
    kind: "admin" | "active";
  } | null>(null);

  const createMutation = useMutation({
    mutationFn: (body: { username: string; password: string }) =>
      api<AdminUser>("/api/admin/users", { method: "POST", body }),
    onSuccess: async () => {
      setCreateOpen(false);
      setUsername("");
      setPassword("");
      await qc.invalidateQueries({ queryKey: ["admin", "users"] });
    },
  });

  const updateMutation = useMutation({
    mutationFn: (vars: { id: string; isPlatformAdmin?: boolean; disabled?: boolean }) =>
      api<AdminUser>(`/api/admin/users/${vars.id}`, {
        method: "PATCH",
        body: {
          isPlatformAdmin: vars.isPlatformAdmin,
          disabled: vars.disabled,
        },
      }),
    onSuccess: async () => {
      setConfirmTarget(null);
      setActionError(null);
      await qc.invalidateQueries({ queryKey: ["admin", "users"] });
    },
    onError: (err) => {
      setConfirmTarget(null);
      setActionError(err);
    },
  });

  function handleOpenCreate() {
    createMutation.reset();
    setUsername("");
    setPassword("");
    setCreateOpen(true);
  }

  function handleCreateSubmit(e: FormEvent) {
    e.preventDefault();
    createMutation.mutate({ username: username.trim(), password });
  }

  const createApiErr = createMutation.error instanceof ApiError ? createMutation.error : null;

  return (
    <div className="stack">
      <PageHeader
        title="Users"
        description="Tambah user baru, kelola hak Platform Admin, dan nonaktifkan akun."
        actions={
          <Button onClick={handleOpenCreate}>
            + Tambah User
          </Button>
        }
      />

      {actionError != null && <Alert tone="danger" error={actionError} />}

      <Panel title="Daftar User" flush>
        <QueryState
          query={usersQuery}
          skeleton={<SkeletonTable rows={5} columns={5} />}
          isEmpty={(d) => d.items.length === 0}
          empty={<EmptyState title="Belum ada user" description="Tambahkan user pertama untuk mulai menggunakan platform." />}
        >
          {(d) => (
            <DataTable
              caption="Daftar user platform"
              rows={d.items}
              rowKey={(u) => u.id}
              columns={[
                {
                  header: "Username",
                  cell: (u) => (
                    <div style={{ display: "flex", alignItems: "center", gap: ".5rem", flexWrap: "wrap" }}>
                      <strong style={{ fontFamily: "var(--mono)" }}>{u.username}</strong>
                      {u.id === currentUser.id && <span className="chip">Anda</span>}
                    </div>
                  ),
                },
                {
                  header: "Peran",
                  cell: (u) =>
                    u.isPlatformAdmin ? (
                      <StatusBadge label="Platform Admin" tone="info" />
                    ) : (
                      <span className="chip">User Biasa</span>
                    ),
                },
                {
                  header: "Status",
                  cell: (u) => (
                    <StatusBadge
                      label={u.disabled ? "Nonaktif" : "Aktif"}
                      tone={u.disabled ? "danger" : "ok"}
                    />
                  ),
                },
                {
                  header: "Dibuat",
                  className: "nowrap",
                  cell: (u) => formatDateTime(u.createdAt),
                },
                {
                  header: "Aksi",
                  className: "nowrap",
                  cell: (u) => {
                    const isSelf = u.id === currentUser.id;
                    return (
                      <div style={{ display: "flex", gap: ".5rem", flexWrap: "wrap" }}>
                        <Button
                          variant="line"
                          size="sm"
                          disabled={isSelf || updateMutation.isPending}
                          title={isSelf ? "Tidak bisa mengubah status admin diri sendiri" : undefined}
                          onClick={() => setConfirmTarget({ user: u, kind: "admin" })}
                        >
                          {u.isPlatformAdmin ? "Cabut Admin" : "Jadikan Admin"}
                        </Button>
                        <Button
                          variant={u.disabled ? "line" : "danger"}
                          size="sm"
                          disabled={isSelf || updateMutation.isPending}
                          title={isSelf ? "Tidak bisa menonaktifkan akun sendiri" : undefined}
                          onClick={() => setConfirmTarget({ user: u, kind: "active" })}
                        >
                          {u.disabled ? "Aktifkan" : "Nonaktifkan"}
                        </Button>
                      </div>
                    );
                  },
                },
              ]}
            />
          )}
        </QueryState>
      </Panel>

      <Dialog
        open={createOpen}
        onClose={() => !createMutation.isPending && setCreateOpen(false)}
        title="Tambah User Baru"
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
              form="create-user-form"
              loading={createMutation.isPending}
            >
              Tambah User
            </Button>
          </>
        }
      >
        <form id="create-user-form" onSubmit={handleCreateSubmit} noValidate>
          {createMutation.isError && (
            <div style={{ marginBottom: "1rem" }}>
              <Alert tone="danger" error={createMutation.error} />
            </div>
          )}
          <Field label="Username" error={createApiErr?.fieldError("username")}>
            <input
              className="input"
              name="username"
              autoComplete="off"
              required
              value={username}
              onChange={(e) => setUsername(e.target.value)}
            />
          </Field>
          <Field label="Password" error={createApiErr?.fieldError("password")}>
            <input
              className="input"
              type="password"
              name="password"
              autoComplete="new-password"
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </Field>
        </form>
      </Dialog>

      <ConfirmDialog
        open={confirmTarget != null}
        title={
          confirmTarget?.kind === "admin"
            ? confirmTarget.user.isPlatformAdmin
              ? "Cabut Hak Platform Admin"
              : "Jadikan Platform Admin"
            : confirmTarget?.user.disabled
              ? "Aktifkan Akun User"
              : "Nonaktifkan Akun User"
        }
        confirmLabel={
          confirmTarget?.kind === "admin"
            ? confirmTarget.user.isPlatformAdmin
              ? "Cabut Admin"
              : "Jadikan Admin"
            : confirmTarget?.user.disabled
              ? "Aktifkan"
              : "Nonaktifkan"
        }
        danger={
          (confirmTarget?.kind === "admin" && confirmTarget.user.isPlatformAdmin) ||
          (confirmTarget?.kind === "active" && !confirmTarget.user.disabled)
        }
        loading={updateMutation.isPending}
        onCancel={() => setConfirmTarget(null)}
        onConfirm={() => {
          if (!confirmTarget) return;
          if (confirmTarget.kind === "admin") {
            updateMutation.mutate({
              id: confirmTarget.user.id,
              isPlatformAdmin: !confirmTarget.user.isPlatformAdmin,
            });
          } else {
            updateMutation.mutate({
              id: confirmTarget.user.id,
              disabled: !confirmTarget.user.disabled,
            });
          }
        }}
      >
        {confirmTarget?.kind === "admin" ? (
          <p>
            Apakah Anda yakin ingin{" "}
            {confirmTarget.user.isPlatformAdmin
              ? "mencabut hak Platform Admin dari"
              : "memberikan hak Platform Admin kepada"}{" "}
            <strong>{confirmTarget.user.username}</strong>?
          </p>
        ) : (
          <p>
            Apakah Anda yakin ingin{" "}
            {confirmTarget?.user.disabled ? "mengaktifkan kembali" : "menonaktifkan"}{" "}
            akun <strong>{confirmTarget?.user.username}</strong>?
            {!confirmTarget?.user.disabled && " Semua session aktif user ini akan langsung dicabut."}
          </p>
        )}
      </ConfirmDialog>
    </div>
  );
}

