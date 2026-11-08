export type JobStatus = "queued" | "running" | "success" | "failed" | "partial" | "uncertain" | "superseded";

export interface Job {
  id: string;
  databaseId: string;
  databaseName: string;
  migrationId: string;
  migrationVersion: number;
  description: string;
  approvalId: string;
  action: string;
  resumesJobId: string | null;
  status: JobStatus;
  attempt: number;
  maxAttempts: number;
  error: string;
  batchesCommitted: number;
  rowsCommitted: number;
  createdBy: string;
  createdAt: string;
  updatedAt: string;
  runAfter: string;
  startedAt: string | null;
  finishedAt: string | null;
}

export function isActive(status: JobStatus): boolean {
  return status === "queued" || status === "running" || status === "uncertain";
}
