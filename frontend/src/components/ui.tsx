"use client";

import { useState, type ReactNode } from "react";
import {
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  CircleDashed,
  Info,
  Loader2,
  XCircle,
} from "lucide-react";
import type { Stage } from "@/lib/api";
import { ago } from "@/lib/format";
import { useNow } from "@/lib/usePoll";

export function cx(...c: (string | false | null | undefined)[]) {
  return c.filter(Boolean).join(" ");
}

export function PageHeader({ title, subtitle, actions }: { title: string; subtitle?: ReactNode; actions?: ReactNode }) {
  return (
    <div className="mb-7 flex flex-wrap items-end justify-between gap-4 border-b border-line/50 pb-5">
      <div className="min-w-0">
        <h1 className="text-2xl sm:text-3xl font-bold tracking-tight text-ink">{title}</h1>
        {subtitle && <p className="mt-1.5 max-w-3xl text-sm sm:text-base text-ink-2 font-normal leading-relaxed">{subtitle}</p>}
      </div>
      {actions && <div className="flex items-center gap-2.5">{actions}</div>}
    </div>
  );
}

export function Card({ title, subtitle, actions, children, className, pad = true }: {
  title?: ReactNode;
  subtitle?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
  className?: string;
  pad?: boolean;
}) {
  return (
    <section className={cx("rounded-2xl border border-line bg-surface shadow-xs transition-shadow hover:shadow-sm", className)}>
      {(title || actions) && (
        <header className="flex items-start justify-between gap-3 border-b border-line px-6 py-4">
          <div className="min-w-0">
            {title && <h2 className="text-base sm:text-lg font-bold tracking-tight text-ink">{title}</h2>}
            {subtitle && <p className="mt-0.5 text-xs sm:text-sm text-ink-3 font-normal">{subtitle}</p>}
          </div>
          {actions}
        </header>
      )}
      <div className={pad ? "p-6" : undefined}>{children}</div>
    </section>
  );
}

export function StatTile({ label, value, hint, accent }: { label: string; value: ReactNode; hint?: ReactNode; accent?: string }) {
  return (
    <div className="rounded-2xl border border-line bg-surface p-4 sm:p-5 shadow-xs transition hover:shadow-sm hover:border-line-strong">
      <div className="flex items-center gap-2 text-xs sm:text-sm font-semibold tracking-wide text-ink-3 uppercase">
        {accent && <span className="h-2.5 w-2.5 rounded-full" style={{ background: accent }} />}
        {label}
      </div>
      <div className="mt-2 text-2xl sm:text-3xl font-bold tracking-tight text-ink">{value}</div>
      {hint && <div className="mt-1 text-xs sm:text-sm text-ink-3">{hint}</div>}
    </div>
  );
}

const STAGE_STYLE: Record<Stage, string> = {
  production: "bg-emerald-500/15 text-emerald-700 dark:text-emerald-400 ring-emerald-500/30",
  staging: "bg-indigo-500/15 text-indigo-700 dark:text-indigo-400 ring-indigo-500/30",
  registered: "bg-surface-2 text-ink-2 ring-line-strong",
  archived: "bg-surface-2 text-ink-3 ring-line",
};

export function StageBadge({ stage }: { stage: Stage }) {
  return (
    <span className={cx("inline-flex items-center gap-1.5 rounded-full px-3 py-1 text-xs font-semibold capitalize ring-1 ring-inset", STAGE_STYLE[stage])}>
      {stage === "production" && <CheckCircle2 size={13} aria-hidden />}
      {stage}
    </span>
  );
}

export type Health = "up" | "down" | "unknown";

export function HealthPill({ state, label }: { state: Health; label?: string }) {
  const map = {
    up: { Icon: CheckCircle2, cls: "text-emerald-700 dark:text-emerald-400 bg-emerald-500/10 ring-1 ring-emerald-500/25", text: label ?? "Healthy" },
    down: { Icon: XCircle, cls: "text-rose-700 dark:text-rose-400 bg-rose-500/10 ring-1 ring-rose-500/25", text: label ?? "Down" },
    unknown: { Icon: CircleDashed, cls: "text-ink-3 bg-surface-2 ring-1 ring-line", text: label ?? "Checking" },
  }[state];
  return (
    <span className={cx("inline-flex items-center gap-1.5 rounded-full px-3 py-1 text-xs sm:text-sm font-semibold", map.cls)}>
      <map.Icon size={15} aria-hidden />
      {map.text}
    </span>
  );
}

export function LiveBadge({ updatedAt, error, intervalLabel }: { updatedAt?: number; error?: Error; intervalLabel?: string }) {
  const now = useNow(1000);
  if (error && !updatedAt) {
    return (
      <span className="inline-flex items-center gap-1.5 rounded-full border border-critical/40 bg-critical/10 px-3 py-1 text-xs sm:text-sm font-medium text-critical">
        <XCircle size={14} aria-hidden /> Offline
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-2 rounded-full border border-line bg-surface px-3 py-1 text-xs sm:text-sm font-medium text-ink-2 shadow-xs">
      <span className={cx("h-2 w-2 rounded-full", error ? "bg-warning" : "bg-emerald-500 live-dot")} />
      {error ? "Reconnecting" : "Live"}
      <span className="text-ink-3">· {updatedAt ? ago(updatedAt, now) : "loading"}{intervalLabel ? ` · every ${intervalLabel}` : ""}</span>
    </span>
  );
}

/** Collapsible "what is this?" panel at the top of each page. */
export function Explainer({ title, children, defaultOpen = false }: { title: string; children: ReactNode; defaultOpen?: boolean }) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <div className="mb-6 rounded-2xl border border-accent/25 bg-accent-soft/50 shadow-xs">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="flex w-full items-center gap-2.5 px-5 py-3.5 text-left text-sm sm:text-base font-semibold text-ink"
        aria-expanded={open}
      >
        <Info size={18} className="shrink-0 text-accent" aria-hidden />
        <span className="flex-1">{title}</span>
        <ChevronDown size={18} className={cx("text-ink-3 transition-transform", open && "rotate-180")} aria-hidden />
      </button>
      {open && <div className="space-y-2.5 px-5 pb-5 pl-11 text-sm sm:text-base leading-relaxed text-ink-2">{children}</div>}
    </div>
  );
}

export function Button({ children, variant = "primary", loading, className, ...props }: React.ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "primary" | "secondary" | "ghost" | "danger";
  loading?: boolean;
}) {
  const styles = {
    primary: "bg-accent text-white hover:brightness-110 shadow-sm hover:shadow",
    secondary: "border border-line-strong bg-surface text-ink hover:bg-surface-2 shadow-xs",
    ghost: "text-ink-2 hover:bg-surface-2 hover:text-ink",
    danger: "border border-critical/40 bg-critical/10 text-critical hover:bg-critical/20",
  }[variant];
  return (
    <button
      {...props}
      disabled={props.disabled || loading}
      className={cx(
        "inline-flex items-center justify-center gap-2 rounded-xl px-4 py-2 text-sm sm:text-base font-semibold transition active:scale-[0.98] disabled:cursor-not-allowed disabled:opacity-50",
        styles,
        className,
      )}
    >
      {loading && <Loader2 size={16} className="animate-spin" aria-hidden />}
      {children}
    </button>
  );
}

export function EmptyState({ icon, title, children }: { icon?: ReactNode; title: string; children?: ReactNode }) {
  return (
    <div className="flex flex-col items-center justify-center px-6 py-14 text-center">
      {icon && <div className="mb-3 text-ink-3">{icon}</div>}
      <p className="text-base sm:text-lg font-bold text-ink">{title}</p>
      {children && <div className="mt-1.5 max-w-lg text-sm sm:text-base text-ink-3">{children}</div>}
    </div>
  );
}

export function ErrorNote({ error, what }: { error?: Error; what: string }) {
  if (!error) return null;
  return (
    <div className="mb-5 flex items-start gap-2.5 rounded-xl border border-critical/30 bg-critical/10 px-4 py-3 text-sm sm:text-base text-ink">
      <AlertTriangle size={18} className="mt-0.5 shrink-0 text-critical" aria-hidden />
      <span>
        Couldn&apos;t reach {what}: <span className="font-semibold text-ink-2">{error.message}</span>
      </span>
    </div>
  );
}

export function Skeleton({ className }: { className?: string }) {
  return <div className={cx("animate-pulse rounded-md bg-surface-2", className)} />;
}

export function Kbd({ children }: { children: ReactNode }) {
  return <code className="rounded bg-surface-2 px-1.5 py-0.5 font-mono text-[12px] text-ink">{children}</code>;
}

/** Model identity color, fixed per model across every chart. */
export function modelColor(name: string) {
  if (name.includes("xgb")) return "var(--series-2)";
  if (name.includes("lr")) return "var(--series-1)";
  return "var(--series-3)";
}
