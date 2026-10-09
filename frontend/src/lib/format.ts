export const pct = (v?: number, digits = 1) => (v == null || Number.isNaN(v) ? "—" : `${(v * 100).toFixed(digits)}%`);

export const num = (v?: number, digits = 3) => (v == null || Number.isNaN(v) ? "—" : v.toFixed(digits));

export function bytes(n: number) {
  if (n < 1024) return `${n} B`;
  if (n < 1024 ** 2) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 ** 2).toFixed(1)} MB`;
}

export function ms(seconds?: number) {
  if (seconds == null || Number.isNaN(seconds)) return "—";
  const v = seconds * 1000;
  return v < 1 ? `${v.toFixed(2)} ms` : v < 10 ? `${v.toFixed(1)} ms` : `${Math.round(v)} ms`;
}

export function ago(ts: string | number | undefined, now: number) {
  if (ts == null || !now) return "—";
  const t = typeof ts === "number" ? ts : Date.parse(ts);
  const s = Math.max(0, Math.round((now - t) / 1000));
  if (s < 5) return "just now";
  if (s < 60) return `${s}s ago`;
  const m = Math.round(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.round(m / 60);
  if (h < 48) return `${h}h ago`;
  return `${Math.round(h / 24)}d ago`;
}

export const dateTime = (ts?: string | null) =>
  ts ? new Date(ts).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" }) : "—";

export const compact = (n: number) => Intl.NumberFormat(undefined, { notation: "compact" }).format(n);
