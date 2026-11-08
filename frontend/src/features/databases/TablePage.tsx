import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { Link, useParams, useSearchParams } from "react-router";
import { api } from "../../lib/api";
import { formatNumber } from "../../lib/format";
import { Button } from "../../components/Button";
import { DataTable } from "../../components/DataTable";
import { PageHeader } from "../../components/PageHeader";
import { Panel } from "../../components/Panel";
import { QueryState } from "../../components/QueryState";
import { SkeletonTable } from "../../components/Skeleton";
import { EmptyState } from "../../components/States";
import type { TableDetail } from "./types";

export default function TablePage() {
  const { dbId = "", schema = "", table = "" } = useParams();
  const [params, setParams] = useSearchParams();
  const page = Math.max(1, Number(params.get("page")) || 1);

  const query = useQuery({
    queryKey: ["databases", dbId, "tables", schema, table, page],
    queryFn: () =>
      api<TableDetail>(`/api/databases/${dbId}/tables/${encodeURIComponent(schema)}/${encodeURIComponent(table)}`, {
        query: { page },
      }),
    placeholderData: keepPreviousData,
  });

  return (
    <QueryState query={query} skeleton={<SkeletonTable rows={8} columns={4} />}>
      {(d) => (
        <div className="stack">
          <PageHeader
            title={d.table}
            description={
              <>
                <Link to="/databases">Databases</Link> / <Link to={`/databases/${d.database.id}`}>{d.database.name}</Link> /{" "}
                <span className="chip">{d.schema}</span>
              </>
            }
          />

          <Panel title="Data" flush footer={<Pager page={d.rows.page} pages={d.rows.pages} total={d.rows.total} busy={query.isPlaceholderData} onChange={(p) => setParams({ page: String(p) })} />}>
            {d.rows.total === 0 ? (
              <EmptyState title="Tabel ini kosong" />
            ) : (
              <DataTable
                caption={`Isi tabel ${d.table}`}
                rows={d.rows.rows.map((cells, i) => ({ i, cells }))}
                rowKey={(r) => String(r.i)}
                columns={d.rows.columns.map((name, c) => ({
                  header: name,
                  cell: (r: { cells: (string | null)[] }) =>
                    r.cells[c] === null ? <em style={{ color: "var(--muted)" }}>NULL</em> : <span style={{ fontFamily: "var(--mono)" }}>{r.cells[c]}</span>,
                }))}
              />
            )}
          </Panel>

          <Panel title="Kolom" flush>
            <DataTable
              caption="Kolom"
              rows={d.columns}
              rowKey={(c) => c.name}
              columns={[
                { header: "Nama", cell: (c) => <strong style={{ fontFamily: "var(--mono)" }}>{c.name}</strong> },
                { header: "Tipe", cell: (c) => <span className="chip">{c.dataType}</span> },
                { header: "Null", className: "nowrap", cell: (c) => (c.nullable ? "ya" : "tidak") },
                { header: "Default", cell: (c) => c.default && <code>{c.default}</code> },
              ]}
            />
          </Panel>

          {d.constraints.length > 0 && (
            <Panel title="Constraint" flush>
              <DataTable
                caption="Constraint"
                rows={d.constraints}
                rowKey={(c) => c.name}
                columns={[
                  { header: "Nama", cell: (c) => <strong style={{ fontFamily: "var(--mono)" }}>{c.name}</strong> },
                  { header: "Jenis", className: "nowrap", cell: (c) => <span className="chip">{c.kind}</span> },
                  { header: "Definisi", cell: (c) => <code>{c.definition}</code> },
                ]}
              />
            </Panel>
          )}

          {d.indexes.length > 0 && (
            <Panel title="Index" flush>
              <DataTable
                caption="Index"
                rows={d.indexes}
                rowKey={(i) => i.name}
                columns={[
                  { header: "Nama", cell: (i) => <strong style={{ fontFamily: "var(--mono)" }}>{i.name}</strong> },
                  { header: "Jenis", className: "nowrap", cell: (i) => <span className="chip">{i.primary ? "primary" : i.unique ? "unique" : "biasa"}</span> },
                  { header: "Definisi", cell: (i) => <code>{i.definition}</code> },
                ]}
              />
            </Panel>
          )}
        </div>
      )}
    </QueryState>
  );
}

function Pager({ page, pages, total, busy, onChange }: { page: number; pages: number; total: number; busy: boolean; onChange: (p: number) => void }) {
  return (
    <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: ".75rem", flexWrap: "wrap" }}>
      <span style={{ fontSize: ".85rem", color: "var(--muted)" }}>
        {formatNumber(total)} baris · halaman {formatNumber(page)} dari {formatNumber(Math.max(pages, 1))}
      </span>
      <div style={{ display: "flex", gap: ".45rem" }}>
        <Button variant="line" size="sm" disabled={page <= 1 || busy} onClick={() => onChange(page - 1)}>Sebelumnya</Button>
        <Button variant="line" size="sm" disabled={page >= pages || busy} onClick={() => onChange(page + 1)}>Berikutnya</Button>
      </div>
    </div>
  );
}
