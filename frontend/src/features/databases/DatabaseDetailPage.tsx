import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router";
import { api } from "../../lib/api";
import { DataTable } from "../../components/DataTable";
import { PageHeader } from "../../components/PageHeader";
import { Panel } from "../../components/Panel";
import { QueryState } from "../../components/QueryState";
import { SkeletonTable } from "../../components/Skeleton";
import { EmptyState } from "../../components/States";
import { StatusBadge } from "../../components/StatusBadge";
import { levelLabel, type DatabaseDetail } from "./types";

export default function DatabaseDetailPage() {
  const { dbId = "" } = useParams();
  const query = useQuery({
    queryKey: ["databases", dbId],
    queryFn: () => api<DatabaseDetail>(`/api/databases/${dbId}`),
  });

  return (
    <QueryState query={query} skeleton={<SkeletonTable rows={6} columns={3} />}>
      {(db) => (
        <div className="stack">
          <PageHeader
            title={db.name}
            description={
              <>
                <Link to="/databases">Databases</Link> / <span className="chip">{db.database}</span>{" "}
                <StatusBadge label={levelLabel[db.level]} tone={db.level === "view_edit" ? "info" : "neutral"} />
              </>
            }
            actions={
              <>
                <Link className="btn btn-line" to={`/jobs?databaseId=${db.id}`}>Job</Link>
                {/* Halaman migration masih Go template, jadi perlu muat ulang penuh. */}
                <a className="btn btn-accent" href={`/databases/${db.id}/migrations`}>Riwayat migration</a>
              </>
            }
          />
          {db.schemas.length === 0 ? (
            <Panel><EmptyState title="Belum ada schema" /></Panel>
          ) : (
            db.schemas.map((schema) => (
              <Panel key={schema.name} title={schema.name} flush>
                {schema.tables.length === 0 ? (
                  <EmptyState title="Schema ini belum punya tabel" />
                ) : (
                  <DataTable
                    caption={`Tabel di schema ${schema.name}`}
                    rows={schema.tables}
                    rowKey={(t) => t.name}
                    columns={[
                      {
                        header: "Tabel",
                        cell: (t) => (
                          <Link to={`/databases/${db.id}/tables/${encodeURIComponent(schema.name)}/${encodeURIComponent(t.name)}`}>
                            <strong>{t.name}</strong>
                          </Link>
                        ),
                      },
                      { header: "Jenis", className: "nowrap", cell: (t) => <span className="chip">{t.kind}</span> },
                      { header: "Kolom", className: "nowrap", cell: (t) => t.columns },
                    ]}
                  />
                )}
              </Panel>
            ))
          )}
        </div>
      )}
    </QueryState>
  );
}
