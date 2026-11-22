import type { ReactNode } from "react";

interface PanelProps {
  title?: string;
  actions?: ReactNode;
  footer?: ReactNode;
  flush?: boolean;
  children: ReactNode;
}

export function Panel({ title, actions, footer, flush, children }: PanelProps) {
  return (
    <section className="panel" aria-label={title}>
      {(title || actions) && (
        <div className="panel-bar">
          <span className="dots" aria-hidden="true">
            <i />
            <i />
            <i />
          </span>
          {title && <span className="panel-tab">{title}</span>}
          {actions && <div className="panel-actions">{actions}</div>}
        </div>
      )}
      <div className={flush ? "panel-body flush" : "panel-body"}>{children}</div>
      {footer && <div className="panel-foot">{footer}</div>}
    </section>
  );
}