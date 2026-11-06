export type ApprovalStatus = "pending_review" | "approved" | "rejected" | "cancelled";
export type ApprovalAction = "apply" | "rollback" | "forward" | "batch";

export interface ApprovalGroupDecision {
  groupId: string;
  groupName: string;
  accessLevel: "view_only" | "view_edit";
  leaderId: string | null;
  leaderUsername: string;
  decision: ApprovalStatus;
  decidedBy: string;
  note: string;
  decidedAt: string | null;
}

export interface ApprovalItem {
  id: string;
  migrationId: string;
  databaseId: string;
  databaseName: string;
  version: number;
  description: string;
  sql: string;
  action: ApprovalAction;
  status: ApprovalStatus;
  requestedById: string;
  requestedBy: string;
  rejectionReason: string;
  createdAt: string;
  updatedAt: string;
  resolvedAt: string | null;
  approvals: ApprovalGroupDecision[];
  approvedCount: number;
  totalRequired: number;
  canDecide: boolean;
  myLeaderGroups: string[];
}

