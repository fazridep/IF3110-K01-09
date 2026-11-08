export interface Connection {
  id: string;
  name: string;
  host: string;
  port: number;
  database: string;
  username: string;
  sslmode: string;
  createdAt: string;
  updatedAt: string;
}

export interface ConnectionForm {
  name: string;
  host: string;
  port: number;
  database: string;
  username: string;
  password: string;
  sslmode: string;
}

export interface TestResult {
  ok: boolean;
  message: string;
}

export const SSL_MODES = ["disable", "allow", "prefer", "require", "verify-ca", "verify-full"];

export const emptyForm: ConnectionForm = {
  name: "",
  host: "",
  port: 5432,
  database: "",
  username: "",
  password: "",
  sslmode: "prefer",
};
