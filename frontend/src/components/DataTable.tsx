import { useLayoutEffect, useRef, type ReactNode } from "react";

export interface Column<T> {
  header: ReactNode;
  cell: (row: T) => ReactNode;
  className?: string;
}

interface DataTableProps<T> {
  columns: Column<T>[];
  rows: T[];
  rowKey: (row: T) => string;
  caption?: string;
  empty?: ReactNode;
  stack?: boolean;
}

export function DataTable<T>({ columns, rows, rowKey, caption, empty, stack = true }: DataTableProps<T>) {
  const ref = useRef<HTMLDivElement>(null);

  useLayoutEffect(() => {
    const wrap = ref.current;
    if (!wrap || !stack) return;
    let width = -1;
    const fit = () => {
      if (wrap.clientWidth === width) return;
      width = wrap.clientWidth;
      wrap.classList.remove("is-stacked");
      wrap.classList.toggle("is-stacked", wrap.scrollWidth > wrap.clientWidth + 1);
    };
    fit();
    if (typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(fit);
    ro.observe(wrap);
    return () => ro.disconnect();
  }, [stack, rows, columns]);

  if (rows.length === 0 && empty) return <>{empty}</>;
  return (
    <div ref={ref} className="table-wrap" tabIndex={0} role="region" aria-label={caption ?? "Tabel"}>
      <table className="table">
        {caption && <caption className="visually-hidden">{caption}</caption>}
        <thead>
          <tr>
            {columns.map((c, i) => (
              <th key={i} scope="col" className={c.className}>
                {c.header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={rowKey(row)}>
              {columns.map((c, i) => (
                <td key={i} className={c.className} data-label={typeof c.header === "string" && c.header ? c.header : undefined}>
                  {c.cell(row)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}