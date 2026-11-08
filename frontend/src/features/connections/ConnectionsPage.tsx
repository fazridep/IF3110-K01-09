import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type List } from "../../lib/api";
import { formatDateTime } from "../../lib/format";
import { Button } from "../../components/Button";
import { DataTable } from "../../components/DataTable";
import { ConfirmDialog } from "../../components/Dialog";
import { PageHeader } from "../../components/PageHeader";
import { Panel } from "../../components/Panel";
import { QueryState } from "../../components/QueryState";
import { SkeletonTable } from "../../components/Skeleton";
import { Alert, EmptyState } from "../../components/States";
import { ConnectionDialog } from "./ConnectionDialog";
import type { Connection, TestResult } from "./types";

export default function ConnectionsPage() {
  const qc = useQueryClient();
  const query = useQuery({
    queryKey: ["admin", "connections"],
    queryFn: () => api<List<Connection>>("/api/admin/connections"),
  });

  const [dialog, setDialog] = useState<{ open: boolean; editing: Connection | null }>({ open: false, editing: null });
  const [deleting, setDeleting] = useState<Connection | null>(null);
  const [testResult, setTestResult] = useState<(TestResult & { name: string }) | null>(null);
  const [actionError, setActionError] = useState<unknown>(null);

  const remove = useMutation({
    mutationFn: (id: string) => api(`/api/admin/connections/${id}`, { method: "DELETE" }),
    onSuccess: async () => {
      setActionError(null);
      await qc.invalidateQueries({ queryKey: ["admin", "connections"] });
    },
    onError: setActionError,
    onSettled: () => setDeleting(null),
  });

  const test = useMutation({
    mutationFn: (c: Connection) => api<TestResult>(`/api/admin/connections/${c.id}/test`, { method: "POST" }),
    onSuccess: (res, c) => {
      setActionError(null);
      setTestResult({ ...res, name: c.name });
    },
    onError: setActionError,
  });

  return (
    <div className="stack">
      <PageHeader
        title="Koneksi DB"
        description="Koneksi ke database PostgreSQL target. Password disimpan terenkripsi dan nggak pernah ditampilkan lagi."
        actions={<Button onClick={() => setDialog({ open: true, editing: null })}>+ Koneksi Baru</Button>}
      />

      {actionError != null && <Alert tone="danger" error={actionError} />}
      {testResult && (
        <Alert tone={testResult.ok ? "ok" : "danger"} title={testResult.name}>{testResult.message}</Alert>
      )}

      <Panel title="Daftar Koneksi" flush>
        <QueryState
          query={query}
          skeleton={<SkeletonTable rows={4} columns={5} />}
          isEmpty={(d) => d.items.length === 0}
          empty={<EmptyState title="Belum ada koneksi" description="Tambahkan database target pertama untuk mulai dipakai." />}
        >
          {(d) => (
            <DataTable
              caption="Daftar koneksi database"
              rows={d.items}
              rowKey={(c) => c.id}
              columns={[
                { header: "Nama", cell: (c) => <strong style={{ fontFamily: "var(--mono)" }}>{c.name}</strong> },
                { header: "Alamat", cell: (c) => <span className="chip">{c.host}:{c.port}/{c.database}</span> },
                { header: "Username", cell: (c) => c.username },
                { header: "SSL", className: "nowrap", cell: (c) => <span className="chip">{c.sslmode}</span> },
                { header: "Diubah", className: "nowrap", cell: (c) => formatDateTime(c.updatedAt) },
                {
                  header: "Aksi",
                  className: "nowrap",
                  cell: (c) => (
                    <div style={{ display: "flex", gap: ".45rem", flexWrap: "wrap" }}>
                      <Button variant="line" size="sm" loading={test.isPending && test.variables?.id === c.id} onClick={() => test.mutate(c)}>Tes</Button>
                      <Button variant="line" size="sm" onClick={() => setDialog({ open: true, editing: c })}>Ubah</Button>
                      <Button variant="danger" size="sm" onClick={() => setDeleting(c)}>Hapus</Button>
                    </div>
                  ),
                },
              ]}
            />
          )}
        </QueryState>
      </Panel>

      <ConnectionDialog open={dialog.open} editing={dialog.editing} onClose={() => setDialog({ open: false, editing: null })} />

      <ConfirmDialog
        open={deleting != null}
        title="Hapus koneksi?"
        danger
        confirmLabel="Hapus"
        loading={remove.isPending}
        onCancel={() => setDeleting(null)}
        onConfirm={() => deleting && remove.mutate(deleting.id)}
      >
        Koneksi <strong>{deleting?.name}</strong> dan semua grant-nya akan dihapus. Koneksi yang punya riwayat migration nggak bisa dihapus.
      </ConfirmDialog>
    </div>
  );
}
