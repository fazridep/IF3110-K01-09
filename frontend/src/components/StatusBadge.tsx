export type Tone = "ok" | "warn" | "danger" | "info" | "neutral";

const known: Record<string, { label: string; tone: Tone; live?: boolean }> = {
  draft: { label: "Draft", tone: "neutral" },
  pending_review: { label: "Pending review", tone: "warn" },
  approved: { label: "Approved", tone: "info" },
  rejected: { label: "Ditolak", tone: "danger" },
  queued: { label: "Antre", tone: "info" },
  pending: { label: "Antre", tone: "info" },
  running: { label: "Berjalan", tone: "info", live: true },
  retrying: { label: "Retry", tone: "warn", live: true },
  success: { label: "Sukses", tone: "ok" },
  succeeded: { label: "Sukses", tone: "ok" },
  failed: { label: "Gagal", tone: "danger" },
  partial: { label: "Partial", tone: "warn" },
  partially_reverted: { label: "Partially reverted", tone: "warn" },
  rolled_back: { label: "Di-rollback", tone: "neutral" },
  uncertain: { label: "Belum pasti", tone: "warn" },
  cancelled: { label: "Dibatalkan", tone: "neutral" },
  ok: { label: "OK", tone: "ok" },
};

export function statusInfo(status: string) {
  return known[status.toLowerCase()] ?? { label: status, tone: "neutral" as Tone };
}

interface BadgeProps {
  status?: string;
  label?: string;
  tone?: Tone;
  live?: boolean;
}

export function StatusBadge({ status, label, tone, live }: BadgeProps) {
  const info = status ? statusInfo(status) : { label: label ?? "", tone: tone ?? "neutral", live };
  const isLive = live ?? info.live;
  return (
    <span className={`badge tone-${tone ?? info.tone}${isLive ? " is-live" : ""}`}>
      <span className="dot" aria-hidden="true" />
      {label ?? info.label}
    </span>
  );
}