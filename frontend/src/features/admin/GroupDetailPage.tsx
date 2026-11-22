import { useState, type FormEvent } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../../lib/api";
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
import type { AdminGroup, GroupDetail, GroupGrant, GroupMember } from "./types";

export default function GroupDetailPage() {
  const { groupId = "" } = useParams<{ groupId: string }>();
  const navigate = useNavigate();
  const qc = useQueryClient();

  const detailQuery = useQuery({
    queryKey: ["groups", groupId],
    queryFn: () => api<GroupDetail>(`/api/admin/groups/${groupId}`),
    enabled: Boolean(groupId),
  });

  const [editOpen, setEditOpen] = useState(false);
  const [editName, setEditName] = useState("");
  const [editDescription, setEditDescription] = useState("");

  const [addMemberOpen, setAddMemberOpen] = useState(false);
  const [selectedUserId, setSelectedUserId] = useState("");

  const [grantOpen, setGrantOpen] = useState(false);
  const [selectedDbId, setSelectedDbId] = useState("");
  const [selectedLevel, setSelectedLevel] = useState<"view_only" | "view_edit">("view_only");

  const [deleteGroupOpen, setDeleteGroupOpen] = useState(false);
  const [removingMember, setRemovingMember] = useState<GroupMember | null>(null);
  const [revokingGrant, setRevokingGrant] = useState<GroupGrant | null>(null);
  const [actionError, setActionError] = useState<unknown>(null);

  async function refreshAll() {
    await qc.invalidateQueries({ queryKey: ["groups"] });
  }

  const updateGroupMutation = useMutation({
    mutationFn: (body: { name: string; description: string }) =>
      api<AdminGroup>(`/api/admin/groups/${groupId}`, { method: "PATCH", body }),
    onSuccess: async () => {
      setEditOpen(false);
      setActionError(null);
      await refreshAll();
    },
  });

  const deleteGroupMutation = useMutation({
    mutationFn: () => api<void>(`/api/admin/groups/${groupId}`, { method: "DELETE" }),
    onSuccess: async () => {
      setDeleteGroupOpen(false);
      await qc.invalidateQueries({ queryKey: ["groups"] });
      navigate("/admin/groups");
    },
    onError: (err) => {
      setDeleteGroupOpen(false);
      setActionError(err);
    },
  });

  const setLeaderMutation = useMutation({
    mutationFn: (userId: string | null) =>
      api<AdminGroup>(`/api/admin/groups/${groupId}/leader`, {
        method: "PUT",
        body: { userId },
      }),
    onSuccess: async () => {
      setActionError(null);
      await refreshAll();
    },
    onError: (err) => setActionError(err),
  });

  const addMemberMutation = useMutation({
    mutationFn: (userId: string) =>
      api(`/api/admin/groups/${groupId}/members`, {
        method: "POST",
        body: { userId },
      }),
    onSuccess: async () => {
      setAddMemberOpen(false);
      setSelectedUserId("");
      setActionError(null);
      await refreshAll();
    },
  });

  const removeMemberMutation = useMutation({
    mutationFn: (userId: string) =>
      api<void>(`/api/admin/groups/${groupId}/members/${userId}`, {
        method: "DELETE",
      }),
    onSuccess: async () => {
      setRemovingMember(null);
      setActionError(null);
      await refreshAll();
    },
    onError: (err) => {
      setRemovingMember(null);
      setActionError(err);
    },
  });

  const setGrantMutation = useMutation({
    mutationFn: (body: { databaseId: string; level: "view_only" | "view_edit" }) =>
      api(`/api/admin/groups/${groupId}/grants`, {
        method: "PUT",
        body,
      }),
    onSuccess: async () => {
      setGrantOpen(false);
      setSelectedDbId("");
      setSelectedLevel("view_only");
      setActionError(null);
      await refreshAll();
    },
  });

  const revokeGrantMutation = useMutation({
    mutationFn: (dbId: string) =>
      api<void>(`/api/admin/groups/${groupId}/grants/${dbId}`, {
        method: "DELETE",
      }),
    onSuccess: async () => {
      setRevokingGrant(null);
      setActionError(null);
      await refreshAll();
    },
    onError: (err) => {
      setRevokingGrant(null);
      setActionError(err);
    },
  });

  function openEditDialog(group: AdminGroup) {
    updateGroupMutation.reset();
    setEditName(group.name);
    setEditDescription(group.description);
    setEditOpen(true);
  }

  function handleEditSubmit(e: FormEvent) {
    e.preventDefault();
    updateGroupMutation.mutate({
      name: editName.trim(),
      description: editDescription.trim(),
    });
  }

  function handleAddMemberSubmit(e: FormEvent) {
    e.preventDefault();
    addMemberMutation.mutate(selectedUserId);
  }

  function handleGrantSubmit(e: FormEvent) {
    e.preventDefault();
    setGrantMutation.mutate({
      databaseId: selectedDbId,
      level: selectedLevel,
    });
  }

  const editApiErr = updateGroupMutation.error instanceof ApiError ? updateGroupMutation.error : null;
  const addMemberApiErr = addMemberMutation.error instanceof ApiError ? addMemberMutation.error : null;
  const grantApiErr = setGrantMutation.error instanceof ApiError ? setGrantMutation.error : null;

  return (
    <div className="stack">
      <div>
        <Link to="/admin/groups" className="btn btn-ghost btn-sm">
          ← Kembali ke Groups
        </Link>
      </div>

      <QueryState
        query={detailQuery}
        skeleton={
          <Panel flush>
            <SkeletonTable rows={4} columns={4} />
          </Panel>
        }
      >
        {(d) => (
          <>
            <PageHeader
              title={d.group.name}
              description={d.group.description || "Kelola leader, anggota, dan hak akses database untuk group ini."}
              actions={
                <>
                  <Button variant="line" onClick={() => openEditDialog(d.group)}>
                    Edit Group
                  </Button>
                  <Button variant="danger" onClick={() => setDeleteGroupOpen(true)}>
                    Hapus Group
                  </Button>
                </>
              }
            />

            {actionError != null && <Alert tone="danger" error={actionError} />}

            <Panel title="Informasi & Leader Group">
              <dl className="kv">
                <dt>Nama Group</dt>
                <dd style={{ fontFamily: "var(--mono)", fontWeight: 600 }}>{d.group.name}</dd>
                <dt>Deskripsi</dt>
                <dd>{d.group.description || "—"}</dd>
                <dt>Leader Group</dt>
                <dd>
                  <div style={{ display: "flex", alignItems: "center", gap: ".6rem", flexWrap: "wrap" }}>
                    {d.group.leaderUsername ? (
                      <>
                        <StatusBadge label={`Leader: ${d.group.leaderUsername}`} tone="info" />
                        <Button
                          variant="line"
                          size="sm"
                          loading={setLeaderMutation.isPending}
                          onClick={() => setLeaderMutation.mutate(null)}
                        >
                          Lepas Leader
                        </Button>
                      </>
                    ) : (
                      <StatusBadge label="Belum ada leader" tone="warn" />
                    )}
                  </div>
                </dd>
                <dt>Aturan Approval</dt>
                <dd style={{ color: "var(--muted)", fontSize: ".9rem" }}>
                  Leader dari setiap group yang memiliki akses ke suatu database wajib menyetujui (approve) perubahan migration sebelum masuk antrean eksekusi.
                </dd>
              </dl>
            </Panel>

            <Panel
              title={`Anggota Group (${d.members.length})`}
              flush
              actions={
                <Button
                  size="sm"
                  disabled={d.nonMembers.length === 0}
                  onClick={() => {
                    addMemberMutation.reset();
                    setSelectedUserId(d.nonMembers[0]?.id ?? "");
                    setAddMemberOpen(true);
                  }}
                >
                  + Tambah Anggota
                </Button>
              }
            >
              <DataTable
                caption="Anggota group"
                rows={d.members}
                rowKey={(m) => m.userId}
                empty={
                  <EmptyState
                    title="Belum ada anggota"
                    description="Tambahkan user ke dalam group ini terlebih dahulu sebelum menunjuk Leader Group."
                  />
                }
                columns={[
                  {
                    header: "Username",
                    cell: (m) => <strong style={{ fontFamily: "var(--mono)" }}>{m.username}</strong>,
                  },
                  {
                    header: "Peran",
                    cell: (m) =>
                      m.isLeader ? (
                        <StatusBadge label="Leader Group" tone="info" />
                      ) : (
                        <span className="chip">Anggota</span>
                      ),
                  },
                  {
                    header: "Ditambahkan",
                    className: "nowrap",
                    cell: (m) => formatDateTime(m.addedAt),
                  },
                  {
                    header: "Aksi",
                    className: "nowrap",
                    cell: (m) => (
                      <div style={{ display: "flex", gap: ".5rem", flexWrap: "wrap" }}>
                        {m.isLeader ? (
                          <Button
                            variant="line"
                            size="sm"
                            loading={setLeaderMutation.isPending}
                            onClick={() => setLeaderMutation.mutate(null)}
                          >
                            Lepas Leader
                          </Button>
                        ) : (
                          <Button
                            variant="line"
                            size="sm"
                            loading={setLeaderMutation.isPending}
                            onClick={() => setLeaderMutation.mutate(m.userId)}
                          >
                            Jadikan Leader
                          </Button>
                        )}
                        <Button
                          variant="danger"
                          size="sm"
                          onClick={() => setRemovingMember(m)}
                        >
                          Keluarkan
                        </Button>
                      </div>
                    ),
                  },
                ]}
              />
            </Panel>

            <Panel
              title={`Akses Database (${d.grants.length})`}
              flush
              actions={
                <Button
                  size="sm"
                  disabled={d.databases.length === 0}
                  onClick={() => {
                    setGrantMutation.reset();
                    setSelectedDbId(d.databases[0]?.id ?? "");
                    setSelectedLevel("view_only");
                    setGrantOpen(true);
                  }}
                >
                  + Atur Akses Database
                </Button>
              }
            >
              <DataTable
                caption="Grant akses database"
                rows={d.grants}
                rowKey={(g) => g.databaseId}
                empty={
                  <EmptyState
                    title="Belum ada akses database"
                    description={
                      d.databases.length === 0
                        ? "Belum ada koneksi database terdaftar di platform."
                        : "Berikan akses View Only atau View + Edit ke database untuk group ini."
                    }
                  />
                }
                columns={[
                  {
                    header: "Database",
                    cell: (g) => <strong style={{ fontFamily: "var(--mono)" }}>{g.databaseName}</strong>,
                  },
                  {
                    header: "Level Akses",
                    cell: (g) =>
                      g.level === "view_edit" ? (
                        <StatusBadge label="View + Edit" tone="ok" />
                      ) : (
                        <StatusBadge label="View Only" tone="neutral" />
                      ),
                  },
                  {
                    header: "Diberikan",
                    className: "nowrap",
                    cell: (g) => formatDateTime(g.grantedAt),
                  },
                  {
                    header: "Aksi",
                    className: "nowrap",
                    cell: (g) => (
                      <div style={{ display: "flex", gap: ".5rem", flexWrap: "wrap" }}>
                        <Button
                          variant="line"
                          size="sm"
                          onClick={() => {
                            setGrantMutation.reset();
                            setSelectedDbId(g.databaseId);
                            setSelectedLevel(g.level === "view_edit" ? "view_only" : "view_edit");
                            setGrantOpen(true);
                          }}
                        >
                          Ubah Level
                        </Button>
                        <Button
                          variant="danger"
                          size="sm"
                          onClick={() => setRevokingGrant(g)}
                        >
                          Cabut
                        </Button>
                      </div>
                    ),
                  },
                ]}
              />
            </Panel>

            <Dialog
              open={editOpen}
              onClose={() => !updateGroupMutation.isPending && setEditOpen(false)}
              title="Edit Group"
              footer={
                <>
                  <Button
                    variant="line"
                    onClick={() => setEditOpen(false)}
                    disabled={updateGroupMutation.isPending}
                  >
                    Batal
                  </Button>
                  <Button
                    type="submit"
                    form="edit-group-form"
                    loading={updateGroupMutation.isPending}
                  >
                    Simpan
                  </Button>
                </>
              }
            >
              <form id="edit-group-form" onSubmit={handleEditSubmit} noValidate>
                {updateGroupMutation.isError && (
                  <div style={{ marginBottom: "1rem" }}>
                    <Alert tone="danger" error={updateGroupMutation.error} />
                  </div>
                )}
                <Field label="Nama" error={editApiErr?.fieldError("name")}>
                  <input
                    className="input"
                    name="name"
                    required
                    value={editName}
                    onChange={(e) => setEditName(e.target.value)}
                  />
                </Field>
                <Field label="Deskripsi" error={editApiErr?.fieldError("description")}>
                  <input
                    className="input"
                    name="description"
                    value={editDescription}
                    onChange={(e) => setEditDescription(e.target.value)}
                  />
                </Field>
              </form>
            </Dialog>

            <Dialog
              open={addMemberOpen}
              onClose={() => !addMemberMutation.isPending && setAddMemberOpen(false)}
              title="Tambah Anggota ke Group"
              footer={
                <>
                  <Button
                    variant="line"
                    onClick={() => setAddMemberOpen(false)}
                    disabled={addMemberMutation.isPending}
                  >
                    Batal
                  </Button>
                  <Button
                    type="submit"
                    form="add-member-form"
                    loading={addMemberMutation.isPending}
                  >
                    Tambah Anggota
                  </Button>
                </>
              }
            >
              <form id="add-member-form" onSubmit={handleAddMemberSubmit} noValidate>
                {addMemberMutation.isError && (
                  <div style={{ marginBottom: "1rem" }}>
                    <Alert tone="danger" error={addMemberMutation.error} />
                  </div>
                )}
                <Field label="User" error={addMemberApiErr?.fieldError("userId")}>
                  <select
                    className="select"
                    name="userId"
                    required
                    value={selectedUserId}
                    onChange={(e) => setSelectedUserId(e.target.value)}
                  >
                    <option value="">— pilih user —</option>
                    {d.nonMembers.map((u) => (
                      <option key={u.id} value={u.id}>
                        {u.username}
                      </option>
                    ))}
                  </select>
                </Field>
              </form>
            </Dialog>

            <Dialog
              open={grantOpen}
              onClose={() => !setGrantMutation.isPending && setGrantOpen(false)}
              title="Atur Akses Database"
              footer={
                <>
                  <Button
                    variant="line"
                    onClick={() => setGrantOpen(false)}
                    disabled={setGrantMutation.isPending}
                  >
                    Batal
                  </Button>
                  <Button
                    type="submit"
                    form="set-grant-form"
                    loading={setGrantMutation.isPending}
                  >
                    Simpan Akses
                  </Button>
                </>
              }
            >
              <form id="set-grant-form" onSubmit={handleGrantSubmit} noValidate>
                {setGrantMutation.isError && (
                  <div style={{ marginBottom: "1rem" }}>
                    <Alert tone="danger" error={setGrantMutation.error} />
                  </div>
                )}
                <Field label="Database" error={grantApiErr?.fieldError("databaseId")}>
                  <select
                    className="select"
                    name="databaseId"
                    required
                    value={selectedDbId}
                    onChange={(e) => setSelectedDbId(e.target.value)}
                  >
                    <option value="">— pilih database —</option>
                    {d.databases.map((db) => (
                      <option key={db.id} value={db.id}>
                        {db.name}
                      </option>
                    ))}
                  </select>
                </Field>
                <Field label="Level Akses" error={grantApiErr?.fieldError("level")}>
                  <select
                    className="select"
                    name="level"
                    required
                    value={selectedLevel}
                    onChange={(e) => setSelectedLevel(e.target.value as "view_only" | "view_edit")}
                  >
                    <option value="view_only">View Only (`view_only`)</option>
                    <option value="view_edit">View + Edit (`view_edit`)</option>
                  </select>
                </Field>
              </form>
            </Dialog>

            <ConfirmDialog
              open={deleteGroupOpen}
              title="Hapus Group"
              confirmLabel="Hapus Group"
              danger
              loading={deleteGroupMutation.isPending}
              onCancel={() => setDeleteGroupOpen(false)}
              onConfirm={() => deleteGroupMutation.mutate()}
            >
              <p>
                Apakah Anda yakin ingin menghapus group <strong>{d.group.name}</strong>?
              </p>
            </ConfirmDialog>

            <ConfirmDialog
              open={removingMember != null}
              title="Keluarkan Anggota"
              confirmLabel="Keluarkan"
              danger
              loading={removeMemberMutation.isPending}
              onCancel={() => setRemovingMember(null)}
              onConfirm={() => {
                if (removingMember) removeMemberMutation.mutate(removingMember.userId);
              }}
            >
              <p>
                Keluarkan <strong>{removingMember?.username}</strong> dari group <strong>{d.group.name}</strong>?
                {removingMember?.isLeader &&
                  " Karena user ini adalah Leader Group, posisi leader group akan menjadi kosong."}
              </p>
            </ConfirmDialog>

            <ConfirmDialog
              open={revokingGrant != null}
              title="Cabut Akses Database"
              confirmLabel="Cabut Akses"
              danger
              loading={revokeGrantMutation.isPending}
              onCancel={() => setRevokingGrant(null)}
              onConfirm={() => {
                if (revokingGrant) revokeGrantMutation.mutate(revokingGrant.databaseId);
              }}
            >
              <p>
                Cabut akses group <strong>{d.group.name}</strong> terhadap database{" "}
                <strong>{revokingGrant?.databaseName}</strong>?
              </p>
            </ConfirmDialog>
          </>
        )}
      </QueryState>
    </div>
  );
}

