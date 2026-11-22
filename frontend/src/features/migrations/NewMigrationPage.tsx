import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "react-router";
import { api } from "../../lib/api";
import { PageHeader } from "../../components/PageHeader";
import { Panel } from "../../components/Panel";
import { MigrationForm } from "./MigrationForm";
import { useDatabase } from "./MigrationsPage";
import type { Migration, MigrationInput } from "./types";

export default function NewMigrationPage() {
  const { dbId = "" } = useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const db = useDatabase(dbId);
  const create = useMutation({
    mutationFn: (body: MigrationInput) => api<Migration>(`/api/databases/${dbId}/migrations`, { method: "POST", body }),
    onSuccess: async (m) => {
      await qc.invalidateQueries({ queryKey: ["migrations", dbId] });
      navigate(`/migrations/${m.id}`);
    },
  });

  return (
    <div className="stack">
      <PageHeader
        title="Migration baru"
        description={
          <>
            <Link to={`/databases/${dbId}/migrations`}>Migration {db.data?.name ?? ""}</Link> / baru. Disimpan sebagai draft, lalu diajukan ke leader group.
          </>
        }
      />
      <Panel>
        <MigrationForm
          submitLabel="Simpan draft"
          saving={create.isPending}
          error={create.error}
          onSubmit={(input) => create.mutate(input)}
          onCancel={() => navigate(`/databases/${dbId}/migrations`)}
        />
      </Panel>
    </div>
  );
}
