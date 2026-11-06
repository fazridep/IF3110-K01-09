const dateTime = new Intl.DateTimeFormat("id-ID", {
  dateStyle: "medium",
  timeStyle: "short",
});

const relative = new Intl.RelativeTimeFormat("id-ID", { numeric: "auto" });

export function formatDateTime(iso: string | undefined | null): string {
  if (!iso) return "—";
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? "—" : dateTime.format(d);
}

export function formatRelative(iso: string | undefined | null, now = Date.now()): string {
  if (!iso) return "—";
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return "—";
  const sec = Math.round((t - now) / 1000);
  const abs = Math.abs(sec);
  if (abs < 45) return relative.format(sec, "second");
  if (abs < 45 * 60) return relative.format(Math.round(sec / 60), "minute");
  if (abs < 22 * 3600) return relative.format(Math.round(sec / 3600), "hour");
  return relative.format(Math.round(sec / 86400), "day");
}

export function formatDuration(ms: number | undefined | null): string {
  if (ms == null || Number.isNaN(ms)) return "—";
  if (ms < 1000) return `${Math.round(ms)} ms`;
  const s = ms / 1000;
  if (s < 60) return `${s.toLocaleString("id-ID", { maximumFractionDigits: 1 })} dtk`;
  const m = Math.floor(s / 60);
  return `${m} mnt ${Math.round(s % 60)} dtk`;
}

export function formatNumber(n: number | undefined | null): string {
  return n == null ? "—" : n.toLocaleString("id-ID");
}

export function initials(name: string): string {
  const parts = name.split(/[\s._-]+/).filter(Boolean);
  const s = parts.length > 1 ? parts[0][0] + parts[1][0] : name.slice(0, 2);
  return s.toUpperCase();
}