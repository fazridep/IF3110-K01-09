import { useState, type FormEvent } from "react";
import { ApiError } from "../../lib/api";
import { Button } from "../../components/Button";
import { Field } from "../../components/Field";
import { Alert } from "../../components/States";
import { defaultBatch, emptyInput, type MigrationInput } from "./types";

interface Props {
  initial?: MigrationInput;
  submitLabel: string;
  saving: boolean;
  error: unknown;
  onSubmit: (input: MigrationInput) => void;
  onCancel?: () => void;
}

const BATCH_UP_EXAMPLE = `UPDATE orders_large o
SET amount = o.amount * 1.1
FROM batch_rows b
WHERE o.id = b.id;`;

const BATCH_DOWN_EXAMPLE = `UPDATE orders_large o
SET amount = b.amount
FROM batch_rows b
WHERE o.id = b.id;`;

// Error per kolom sudah tampil di kolomnya, jangan diulang di alert.
function hasFieldErrors(err: unknown): boolean {
  return err instanceof ApiError && (err.problem.errors?.length ?? 0) > 0;
}

export function MigrationForm({ initial = emptyInput, submitLabel, saving, error, onSubmit, onCancel }: Props) {
  const [form, setForm] = useState<MigrationInput>(initial);
  const batch = form.batch ?? defaultBatch;
  const isBatch = form.mode === "batch";

  function set<K extends keyof MigrationInput>(key: K, value: MigrationInput[K]) {
    setForm((f) => ({ ...f, [key]: value }));
  }

  function setBatch<K extends keyof typeof batch>(key: K, value: (typeof batch)[K]) {
    setForm((f) => ({ ...f, batch: { ...(f.batch ?? defaultBatch), [key]: value } }));
  }

  function submit(e: FormEvent) {
    e.preventDefault();
    onSubmit({
      ...form,
      downSql: form.irreversible ? "" : form.downSql,
      irreversibleReason: form.irreversible ? form.irreversibleReason : "",
      batch: isBatch ? batch : null,
    });
  }

  const fieldErr = (name: string) => (error instanceof ApiError ? error.fieldError(name) : undefined);

  return (
    <form onSubmit={submit} noValidate className="stack" style={{ gap: ".9rem" }}>
      {error != null && !hasFieldErrors(error) && <Alert tone="danger" error={error} />}

      <Field label="Deskripsi" error={fieldErr("description")}>
        <input
          className="input"
          autoComplete="off"
          placeholder="Contoh: tambah kolom diskon di orders"
          value={form.description}
          onChange={(e) => set("description", e.target.value)}
        />
      </Field>

      <Field
        label="Mode eksekusi"
        error={fieldErr("mode")}
        hint="Batch memproses tabel besar bertahap. Tiap batch punya transaction sendiri."
      >
        <select
          className="select"
          value={form.mode}
          onChange={(e) => set("mode", e.target.value === "batch" ? "batch" : "normal")}
        >
          <option value="normal">Normal (satu transaction)</option>
          <option value="batch">Per batch</option>
        </select>
      </Field>

      {isBatch && (
        <fieldset className="stack" style={{ gap: ".75rem", border: "1px solid var(--line)", borderRadius: "var(--radius)", padding: ".9rem", margin: 0, minWidth: 0 }}>
          <legend style={{ padding: "0 .35rem", fontWeight: 600 }}>Konfigurasi batch</legend>
          <Field label="Tabel" error={fieldErr("batch.table")} hint="tabel atau schema.tabel">
            <input className="input" autoComplete="off" placeholder="public.orders_large" value={batch.table} onChange={(e) => setBatch("table", e.target.value)} />
          </Field>
          <Field label="Kolom kursor" error={fieldErr("batch.key")} hint="Kolom unik yang nggak berubah, biasanya id.">
            <input className="input" autoComplete="off" value={batch.key} onChange={(e) => setBatch("key", e.target.value)} />
          </Field>
          <Field label="Ukuran batch" error={fieldErr("batch.size")}>
            <input className="input" type="number" min={1} max={1000000} value={batch.size} onChange={(e) => setBatch("size", Number(e.target.value))} />
          </Field>
          <label className="check">
            <input type="checkbox" checked={batch.backup} onChange={(e) => setBatch("backup", e.target.checked)} />
            Simpan data sebelum diubah (dipakai SQL down lewat batch_rows)
          </label>
          <p style={{ margin: 0, fontSize: ".82rem", color: "var(--faint)" }}>
            Tiap batch, row yang diproses ada di temp table <code>batch_rows</code>. SQL up dan down wajib memakainya.
          </p>
        </fieldset>
      )}

      <Field label="SQL up" error={fieldErr("upSql")} hint="Dijalankan waktu apply dan forward.">
        <textarea
          className="textarea code"
          spellCheck={false}
          placeholder={isBatch ? BATCH_UP_EXAMPLE : "CREATE TABLE ..."}
          value={form.upSql}
          onChange={(e) => set("upSql", e.target.value)}
        />
      </Field>

      <label className="check">
        <input type="checkbox" checked={form.irreversible} onChange={(e) => set("irreversible", e.target.checked)} />
        Irreversible (nggak bisa di-rollback)
      </label>

      {form.irreversible ? (
        <Field label="Alasan irreversible" error={fieldErr("irreversibleReason")}>
          <input
            className="input"
            autoComplete="off"
            placeholder="Contoh: kolom lama dihapus, datanya nggak bisa dikembalikan"
            value={form.irreversibleReason}
            onChange={(e) => set("irreversibleReason", e.target.value)}
          />
        </Field>
      ) : (
        <Field label="SQL down" error={fieldErr("downSql")} hint="Dijalankan waktu rollback, membatalkan SQL up.">
          <textarea
            className="textarea code"
            spellCheck={false}
            placeholder={isBatch ? BATCH_DOWN_EXAMPLE : "DROP TABLE ..."}
            value={form.downSql}
            onChange={(e) => set("downSql", e.target.value)}
          />
        </Field>
      )}

      <div style={{ display: "flex", gap: ".5rem", flexWrap: "wrap", justifyContent: "flex-end" }}>
        {onCancel && (
          <Button variant="line" onClick={onCancel} disabled={saving}>
            Batal
          </Button>
        )}
        <Button type="submit" loading={saving}>
          {submitLabel}
        </Button>
      </div>
    </form>
  );
}
