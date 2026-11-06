import type { ReactNode } from "react";
import { ApiError, errorMessage } from "../lib/api";
import { Button } from "./Button";
import type { Tone } from "./StatusBadge";

interface StateProps {
  title: string;
  description?: ReactNode;
  code?: string;
  actions?: ReactNode;
}

export function EmptyState({ title, description, code = "∅", actions }: StateProps) {
  return (
    <div className="state">
      <div className="state-icon" aria-hidden="true">
        {code}
      </div>
      <h3>{title}</h3>
      {description && <p>{description}</p>}
      {actions && <div className="actions">{actions}</div>}
    </div>
  );
}

interface ErrorStateProps extends Partial<StateProps> {
  error?: unknown;
  onRetry?: () => void;
}

export function ErrorState({ error, onRetry, title, description, code, actions }: ErrorStateProps) {
  const problem = error instanceof ApiError ? error.problem : undefined;
  return (
    <div className="state" role="alert" style={{ ["--tone" as string]: "var(--danger)", ["--tone-wash" as string]: "var(--danger-wash)" }}>
      <div className="state-icon" aria-hidden="true">
        {code ?? (problem ? String(problem.status) : "!")}
      </div>
      <h3>{title ?? problem?.title ?? "Gagal memuat data"}</h3>
      <p>{description ?? (error ? errorMessage(error) : null)}</p>
      {problem?.requestId && <span className="meta">request id: {problem.requestId}</span>}
      {(onRetry || actions) && (
        <div className="actions">
          {onRetry && (
            <Button variant="line" size="sm" onClick={onRetry}>
              Coba lagi
            </Button>
          )}
          {actions}
        </div>
      )}
    </div>
  );
}

interface AlertProps {
  tone?: Tone;
  title?: string;
  children?: ReactNode;
  error?: unknown;
}

export function Alert({ tone = "info", title, children, error }: AlertProps) {
  const problem = error instanceof ApiError ? error.problem : undefined;
  const isError = tone === "danger";
  return (
    <div className={`alert tone-${tone}`} role={isError ? "alert" : "status"}>
      <div>
        {title && <span className="alert-title">{title}</span>}
        {children ?? (error ? errorMessage(error) : null)}
        {problem?.requestId && <span className="meta">request id: {problem.requestId}</span>}
      </div>
    </div>
  );
}