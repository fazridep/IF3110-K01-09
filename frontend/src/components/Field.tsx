import { cloneElement, isValidElement, useId, type ReactElement, type ReactNode } from "react";

interface FieldProps {
  label: string;
  error?: string;
  hint?: ReactNode;
  children: ReactElement<Record<string, unknown>>;
}

export function Field({ label, error, hint, children }: FieldProps) {
  const id = useId();
  const hintId = hint ? `${id}-hint` : undefined;
  const errorId = error ? `${id}-error` : undefined;
  const describedBy = [hintId, errorId].filter(Boolean).join(" ") || undefined;

  const control = isValidElement(children)
    ? cloneElement(children, {
        id,
        "aria-invalid": error ? true : undefined,
        "aria-describedby": describedBy,
      })
    : children;

  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      {control}
      {hint && (
        <span id={hintId} className="hint">
          {hint}
        </span>
      )}
      {error && (
        <span id={errorId} className="error">
          {error}
        </span>
      )}
    </div>
  );
}