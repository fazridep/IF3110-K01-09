import type { ButtonHTMLAttributes } from "react";

type Variant = "accent" | "line" | "ghost" | "danger";

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant;
  size?: "md" | "sm";
  block?: boolean;
  loading?: boolean;
}

export function Button({
  variant = "accent",
  size = "md",
  block,
  loading,
  disabled,
  className,
  children,
  type = "button",
  ...rest
}: ButtonProps) {
  const cls = ["btn", `btn-${variant}`, size === "sm" && "btn-sm", block && "btn-block", className]
    .filter(Boolean)
    .join(" ");
  return (
    <button type={type} className={cls} disabled={disabled || loading} aria-busy={loading || undefined} {...rest}>
      {loading && <span className="spinner" aria-hidden="true" />}
      {children}
    </button>
  );
}