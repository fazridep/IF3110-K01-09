import { useEffect, useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../../lib/api";
import { Button } from "../../components/Button";
import { Dialog } from "../../components/Dialog";
import { Field } from "../../components/Field";
import { Alert } from "../../components/States";
import { emptyForm, SSL_MODES, type Connection, type ConnectionForm, type TestResult } from "./types";

interface Props {
  open: boolean;
  editing: Connection | null;
  onClose: () => void;
}

// Error per kolom sudah tampil di kolomnya, jangan diulang di alert.
function hasFieldErrors(err: unknown): boolean {
  return err instanceof ApiError && (err.problem.errors?.length ?? 0) > 0;
}

export function ConnectionDialog({ open, editing, onClose }: Props) {
  const qc = useQueryClient();
  const [form, setForm] = useState<ConnectionForm>(emptyForm);

  const save = useMutation({
    mutationFn: (body: ConnectionForm) =>
      editing
        ? api<Connection>(`/api/admin/connections/${editing.id}`, { method: "PUT", body })
        : api<Connection>("/api/admin/connections", { method: "POST", body }),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["admin", "connections"] });
      onClose();
    },
  });

  const test = useMutation({
    mutationFn: (body: ConnectionForm) =>
      api<TestResult>("/api/admin/connections/test", {
        method: "POST",
        body: editing ? { ...body, id: editing.id } : body,
      }),
  });

  const { reset: resetSave } = save;
  const { reset: resetTest } = test;
  useEffect(() => {
    if (!open) return;
    resetSave();
    resetTest();
    setForm(editing ? { ...editing, password: "" } : emptyForm);
  }, [open, editing, resetSave, resetTest]);

  // id dan tanggal dari Connection nggak ikut dikirim: backend menolak field asing.
  const body = (): ConnectionForm => ({
    name: form.name,
    host: form.host,
    port: form.port,
    database: form.database,
    username: form.username,
    password: form.password,
    sslmode: form.sslmode,
  });

  function set<K extends keyof ConnectionForm>(key: K, value: ConnectionForm[K]) {
    setForm((f) => ({ ...f, [key]: value }));
  }

  function submit(e: FormEvent) {
    e.preventDefault();
    save.mutate(body());
  }

  const fieldErr = (name: string) => {
    for (const err of [save.error, test.error]) {
      if (err instanceof ApiError && err.fieldError(name)) return err.fieldError(name);
    }
    return undefined;
  };
  const busy = save.isPending || test.isPending;

  return (
    <Dialog
      open={open}
      onClose={() => !busy && onClose()}
      title={editing ? `Ubah Koneksi ${editing.name}` : "Koneksi Baru"}
      footer={
        <>
          <Button variant="line" onClick={onClose} disabled={busy}>Batal</Button>
          <Button variant="line" loading={test.isPending} disabled={busy} onClick={() => test.mutate(body())}>Tes Koneksi</Button>
          <Button type="submit" form="connection-form" loading={save.isPending} disabled={busy}>Simpan</Button>
        </>
      }
    >
      <form id="connection-form" onSubmit={submit} noValidate className="stack" style={{ gap: ".75rem" }}>
        {[save.error, test.error].map(
          (err, i) => err != null && !hasFieldErrors(err) && <Alert key={i} tone="danger" error={err} />,
        )}
        {test.data && (
          <Alert tone={test.data.ok ? "ok" : "danger"}>{test.data.message}</Alert>
        )}
        <Field label="Nama koneksi" error={fieldErr("name")}>
          <input className="input" autoComplete="off" value={form.name} onChange={(e) => set("name", e.target.value)} />
        </Field>
        <Field label="Host" error={fieldErr("host")}>
          <input className="input" autoComplete="off" value={form.host} onChange={(e) => set("host", e.target.value)} />
        </Field>
        <Field label="Port" error={fieldErr("port")}>
          <input className="input" type="number" min={1} max={65535} value={form.port} onChange={(e) => set("port", Number(e.target.value))} />
        </Field>
        <Field label="Nama database" error={fieldErr("database")}>
          <input className="input" autoComplete="off" value={form.database} onChange={(e) => set("database", e.target.value)} />
        </Field>
        <Field label="Username" error={fieldErr("username")}>
          <input className="input" autoComplete="off" value={form.username} onChange={(e) => set("username", e.target.value)} />
        </Field>
        <Field label="Password" error={fieldErr("password")} hint={editing ? "Kosongkan kalau nggak mau mengganti password." : undefined}>
          <input className="input" type="password" autoComplete="new-password" value={form.password} onChange={(e) => set("password", e.target.value)} />
        </Field>
        <Field label="SSL mode" error={fieldErr("sslmode")}>
          <select className="select" value={form.sslmode} onChange={(e) => set("sslmode", e.target.value)}>
            {SSL_MODES.map((m) => <option key={m} value={m}>{m}</option>)}
          </select>
        </Field>
      </form>
    </Dialog>
  );
}
