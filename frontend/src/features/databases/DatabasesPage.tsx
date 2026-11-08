import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";
import { api, type List } from "../../lib/api";
import { DataTable } from "../../components/DataTable";
import { PageHeader } from "../../components/PageHeader";
import { Panel } from "../../components/Panel";
import { QueryState } from "../../components/QueryState";
import { SkeletonTable } from "../../components/Skeleton";
import { EmptyState } from "../../components/States";
import { StatusBadge } from "../../components/StatusBadge";
import { levelLabel, type DatabaseItem } from "./types";

export default function DatabasesPage() {
  const query = useQuery({
    queryKey: ["databases"],
    queryFn: () => api<List<DatabaseItem>>("/api/databases"),
  });

  return (
    <div className="stack">
      <PageHeader title="Databases" description="Database yang boleh kamu buka lewat group yang kamu ikuti." />
      <Panel title="Daftar Database" flush>
        <QueryState
          query={query}
          skeleton={<SkeletonTable rows={4} columns={3} />}
          isEmpty={(d) => d.items.length === 0}
          empty={
            <EmptyState
              title="Belum ada database"
              description="Akses database diberikan lewat group. Minta Platform Admin memasukkan kamu ke group yang punya akses."
            />
          }
        >
          {(d) => (
            <DataTable
              caption="Daftar database"
              rows={d.items}
              rowKey={(db) => db.id}
              columns={[
                { header: "Nama", cell: (db) => <Link to={`/databases/${db.id}`}><strong>{db.name}</strong></Link> },
                { header: "Database", cell: (db) => <span className="chip">{db.database}</span> },
                {
                  header: "Akses",
                  className: "nowrap",
                  cell: (db) => <StatusBadge label={levelLabel[db.level]} tone={db.level === "view_edit" ? "info" : "neutral"} />,
                },
              ]}
            />
          )}
        </QueryState>
      </Panel>
    </div>
  );
}
