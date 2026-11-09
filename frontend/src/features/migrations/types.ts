export type MigrationStatus =
  | "draft"
  | "running"
  | "success"
  | "failed"
  | "rolled_back"
  | "partial"
  | "partially_reverted";

export type MigrationMode = "normal" | "batch";
export type MigrationAction = "apply" | "forward" | "rollback" | "batch";

export interface BatchConfig {
  table: string;
  key: string;
  size: number;
  backup: boolean;
}

export interface MigrationInput {
  description: string;
  upSql: string;
  downSql: string;
  irreversible: boolean;
  irreversibleReason: string;
  mode: MigrationMode;
  batch: BatchConfig | null;
}

export interface Migration extends MigrationInput {
  id: string;
  databaseId: string;
  version: number;
  status: MigrationStatus;
  error: string;
  createdBy: string;
  createdAt: string;
  updatedAt: string;
  executedBy: string;
  startedAt: string | null;
  finishedAt: string | null;
  upExecutionId: string | null;
}

export interface MigrationDetail extends Migration {
  canEdit: boolean;
  allowedActions: MigrationAction[];
  blockedReason: string;
}

export type ExecutionStatus = "running" | "success" | "failed" | "partial";

export interface Execution {
  id: string;
  migrationId: string;
  version: number;
  description: string;
  action: "apply" | "forward" | "rollback";
  revertsExecutionId: string | null;
  jobId: string;
  status: ExecutionStatus;
  sql: string;
  mode: MigrationMode;
  batchSize: number;
  batchesCommitted: number;
  rowsCommitted: number;
  cursor: string;
  error: string;
  executedBy: string;
  startedAt: string;
  finishedAt: string | null;
  committed: boolean;
  committedAt: string | null;
}

export const emptyInput: MigrationInput = {
  description: "",
  upSql: "",
  downSql: "",
  irreversible: false,
  irreversibleReason: "",
  mode: "normal",
  batch: null,
};

export const defaultBatch: BatchConfig = { table: "", key: "id", size: 1000, backup: true };

export const actionLabel: Record<string, string> = {
  apply: "Apply",
  forward: "Forward",
  rollback: "Rollback",
  batch: "Lanjutkan batch",
};

export const statusLabel: Record<MigrationStatus, string> = {
  draft: "Draft",
  running: "Berjalan",
  success: "Aktif",
  failed: "Gagal",
  rolled_back: "Di-rollback",
  partial: "Partial",
  partially_reverted: "Partially reverted",
};

// Ringkas satu-dua kata buat badge.
export function shortId(id: string): string {
  return id.slice(0, 8);
}
