import type { ReactNode } from "react";
import type { UseQueryResult } from "@tanstack/react-query";
import { Skeleton } from "./Skeleton";
import { EmptyState, ErrorState } from "./States";

interface QueryStateProps<T> {
  query: UseQueryResult<T>;
  skeleton?: ReactNode;
  isEmpty?: (data: T) => boolean;
  empty?: ReactNode;
  children: (data: T) => ReactNode;
}

export function QueryState<T>({ query, skeleton, isEmpty, empty, children }: QueryStateProps<T>) {
  if (query.isPending) {
    return (
      <div aria-busy="true" aria-live="polite">
        <span className="visually-hidden">Memuat…</span>
        {skeleton ?? <Skeleton lines={3} />}
      </div>
    );
  }
  if (query.isError) {
    return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;
  }
  if (isEmpty?.(query.data)) {
    return <>{empty ?? <EmptyState title="Belum ada data" />}</>;
  }
  return <>{children(query.data)}</>;
}