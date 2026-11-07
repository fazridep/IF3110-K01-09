export interface AdminUser {
  id: string;
  username: string;
  isPlatformAdmin: boolean;
  disabled: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface AdminGroup {
  id: string;
  name: string;
  description: string;
  leaderId: string | null;
  leaderUsername: string;
  memberCount: number;
  createdAt: string;
  updatedAt: string;
}

export interface GroupMember {
  userId: string;
  username: string;
  isLeader: boolean;
  addedAt: string;
}

export interface GroupGrant {
  groupId: string;
  databaseId: string;
  databaseName: string;
  level: "view_only" | "view_edit";
  grantedAt: string;
}

export interface DatabaseOption {
  id: string;
  name: string;
}

export interface GroupDetail {
  group: AdminGroup;
  members: GroupMember[];
  nonMembers: AdminUser[];
  grants: GroupGrant[];
  databases: DatabaseOption[];
}

