import type { CSSProperties } from "react";

export function Skeleton({ lines = 1, width, height }: { lines?: number; width?: CSSProperties["width"]; height?: CSSProperties["height"] }) {
  if (lines <= 1) return <span className="skeleton" style={{ width, height }} aria-hidden="true" />;
  return (
    <span className="skeleton-lines" aria-hidden="true">
      {Array.from({ length: lines }, (_, i) => (
        <span key={i} className="skeleton" style={{ width: i === lines - 1 ? "60%" : width, height }} />
      ))}
    </span>
  );
}

export function SkeletonTable({ rows = 5, columns = 4 }: { rows?: number; columns?: number }) {
  return (
    <div className="table-wrap" aria-hidden="true">
      <table className="table">
        <thead>
          <tr>
            {Array.from({ length: columns }, (_, c) => (
              <th key={c}>
                <Skeleton width="60%" />
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {Array.from({ length: rows }, (_, r) => (
            <tr key={r}>
              {Array.from({ length: columns }, (_, c) => (
                <td key={c}>
                  <Skeleton width={c === 0 ? "80%" : "55%"} />
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}