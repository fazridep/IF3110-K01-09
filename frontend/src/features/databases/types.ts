export type AccessLevel = "view_only" | "view_edit";

export interface DatabaseItem {
  id: string;
  name: string;
  database: string;
  level: AccessLevel;
}

export interface TableItem {
  name: string;
  kind: string;
  columns: number;
}

export interface SchemaItem {
  name: string;
  tables: TableItem[];
}

export interface DatabaseDetail extends DatabaseItem {
  schemas: SchemaItem[];
}

export interface ColumnItem {
  name: string;
  dataType: string;
  nullable: boolean;
  default: string;
}

export interface ConstraintItem {
  name: string;
  kind: string;
  definition: string;
}

export interface IndexItem {
  name: string;
  unique: boolean;
  primary: boolean;
  definition: string;
}

export interface RowsPage {
  columns: string[];
  rows: (string | null)[][];
  total: number;
  page: number;
  perPage: number;
  pages: number;
}

export interface TableDetail {
  database: DatabaseItem;
  schema: string;
  table: string;
  columns: ColumnItem[];
  constraints: ConstraintItem[];
  indexes: IndexItem[];
  rows: RowsPage;
}

export const levelLabel: Record<AccessLevel, string> = {
  view_only: "View Only",
  view_edit: "View + Edit",
};
