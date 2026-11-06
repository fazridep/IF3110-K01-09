import { useEffect, useState, type FormEvent } from "react";
import { Navigate, useNavigate, useSearchParams } from "react-router";
import { useAuth } from "./AuthContext";
import { ApiError } from "../lib/api";
import { Brand } from "../layout/AppLayout";
import { Panel } from "../components/Panel";
import { Field } from "../components/Field";
import { Button } from "../components/Button";
import { Alert } from "../components/States";
import { ThemeToggle } from "../layout/ThemeToggle";

export function safeNext(next: string | null): string | null {
  if (!next || !next.startsWith("/") || next.startsWith("//") || next.startsWith("/\\")) return null;
  if (next.startsWith("/login")) return null;
  return next;
}

export default function LoginPage() {
  const { user, login } = useAuth();
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const next = safeNext(params.get("next"));

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    document.title = "Login · DB Platform";
  }, []);

  if (user && !submitting) return <Navigate to={next ?? "/"} replace />;

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      await login(username.trim(), password);
      if (next) {
        window.location.assign(next);
        return;
      }
      navigate("/", { replace: true });
    } catch (err) {
      setError(err);
      setPassword("");
      setSubmitting(false);
    }
  }

  const apiErr = error instanceof ApiError ? error : null;

  return (
    <div className="bare">
      <div style={{ position: "absolute", top: 16, right: 16 }}>
        <ThemeToggle />
      </div>
      <div className="login-card">
        <div className="login-head">
          <Brand />
          <h1>Masuk ke platform</h1>
          <p>Kelola akses, schema, dan migration database PostgreSQL.</p>
        </div>

        <Panel>
          <form onSubmit={onSubmit} noValidate>
            {error != null && (
              <div style={{ marginBottom: "1rem" }}>
                <Alert tone="danger" error={error} />
              </div>
            )}
            <Field label="Username" error={apiErr?.fieldError("username")}>
              <input
                className="input"
                name="username"
                autoComplete="username"
                autoCapitalize="none"
                spellCheck={false}
                required
                autoFocus
                value={username}
                onChange={(e) => setUsername(e.target.value)}
              />
            </Field>
            <Field label="Password" error={apiErr?.fieldError("password")}>
              <input
                className="input"
                type="password"
                name="password"
                autoComplete="current-password"
                required
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
            </Field>
            <div style={{ marginTop: "1.4rem" }}>
              <Button type="submit" block loading={submitting}>
                {submitting ? "Memeriksa…" : "Login"}
              </Button>
            </div>
          </form>
        </Panel>
      </div>
    </div>
  );
}