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
    <div className="mb-6 flex flex-wrap items-end justify-between gap-4">
      <div className="min-w-0">
        <h1 className="text-2xl font-semibold tracking-tight text-ink">{title}</h1>
        {subtitle && <p className="mt-1 max-w-3xl text-sm text-ink-2">{subtitle}</p>}
      </div>
      {actions && <div className="flex items-center gap-2">{actions}</div>}
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
    <section className={cx("rounded-xl border border-line bg-surface shadow-[0_1px_2px_rgba(0,0,0,0.04)]", className)}>
      {(title || actions) && (
        <header className="flex items-start justify-between gap-3 border-b border-line px-5 py-3.5">
          <div className="min-w-0">
            {title && <h2 className="text-sm font-semibold text-ink">{title}</h2>}
            {subtitle && <p className="mt-0.5 text-xs text-ink-3">{subtitle}</p>}
          </div>
          {actions}
        </header>
      )}
      <div className={pad ? "p-5" : undefined}>{children}</div>
    </section>
  );
}

export function StatTile({ label, value, hint, accent }: { label: string; value: ReactNode; hint?: ReactNode; accent?: string }) {
  return (
    <div className="rounded-xl border border-line bg-surface px-4 py-3.5">
      <div className="flex items-center gap-2 text-xs font-medium text-ink-3">
        {accent && <span className="h-2 w-2 rounded-full" style={{ background: accent }} />}
        {label}
      </div>
      <div className="mt-1.5 text-2xl font-semibold tracking-tight text-ink">{value}</div>
      {hint && <div className="mt-0.5 text-xs text-ink-3">{hint}</div>}
    </div>
  );
}

const STAGE_STYLE: Record<Stage, string> = {
  production: "bg-good/12 text-good-ink ring-good/30",
  staging: "bg-accent-soft text-accent ring-accent/30",
  registered: "bg-surface-2 text-ink-2 ring-line-strong",
  archived: "bg-surface-2 text-ink-3 ring-line",
};

export function StageBadge({ stage }: { stage: Stage }) {
  return (
    <span className={cx("inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-medium capitalize ring-1 ring-inset", STAGE_STYLE[stage])}>
      {stage === "production" && <CheckCircle2 size={11} aria-hidden />}
      {stage}
    </span>
  );
}

export type Health = "up" | "down" | "unknown";

export function HealthPill({ state, label }: { state: Health; label?: string }) {
  const map = {
    up: { Icon: CheckCircle2, cls: "text-good-ink", text: label ?? "Healthy" },
    down: { Icon: XCircle, cls: "text-critical", text: label ?? "Down" },
    unknown: { Icon: CircleDashed, cls: "text-ink-3", text: label ?? "Checking" },
  }[state];
  return (
    <span className={cx("inline-flex items-center gap-1.5 text-xs font-medium", map.cls)}>
      <map.Icon size={14} aria-hidden />
      {map.text}
    </span>
  );
}

export function LiveBadge({ updatedAt, error, intervalLabel }: { updatedAt?: number; error?: Error; intervalLabel?: string }) {
  const now = useNow(1000);
  if (error && !updatedAt) {
    return (
      <span className="inline-flex items-center gap-1.5 rounded-full border border-critical/40 px-2.5 py-1 text-xs text-critical">
        <XCircle size={12} aria-hidden /> Offline
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1.5 rounded-full border border-line px-2.5 py-1 text-xs text-ink-2">
      <span className={cx("h-1.5 w-1.5 rounded-full", error ? "bg-warning" : "bg-good live-dot")} />
      {error ? "Reconnecting" : "Live"}
      <span className="text-ink-3">· {updatedAt ? ago(updatedAt, now) : "loading"}{intervalLabel ? ` · every ${intervalLabel}` : ""}</span>
    </span>
  );
}

/** Collapsible "what is this?" panel at the top of each page. */
export function Explainer({ title, children, defaultOpen = false }: { title: string; children: ReactNode; defaultOpen?: boolean }) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <div className="mb-6 rounded-xl border border-accent/25 bg-accent-soft/60">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="flex w-full items-center gap-2 px-4 py-3 text-left text-sm font-medium text-ink"
        aria-expanded={open}
      >
        <Info size={16} className="shrink-0 text-accent" aria-hidden />
        <span className="flex-1">{title}</span>
        <ChevronDown size={16} className={cx("text-ink-3 transition-transform", open && "rotate-180")} aria-hidden />
      </button>
      {open && <div className="space-y-2 px-4 pb-4 pl-10 text-sm leading-relaxed text-ink-2">{children}</div>}
    </div>
  );
}

export function Button({ children, variant = "primary", loading, className, ...props }: React.ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "primary" | "secondary" | "ghost" | "danger";
  loading?: boolean;
}) {
  const styles = {
    primary: "bg-accent text-white hover:brightness-110",
    secondary: "border border-line-strong bg-surface text-ink hover:bg-surface-2",
    ghost: "text-ink-2 hover:bg-surface-2 hover:text-ink",
    danger: "border border-critical/40 text-critical hover:bg-critical/10",
  }[variant];
  return (
    <button
      {...props}
      disabled={props.disabled || loading}
      className={cx(
        "inline-flex items-center justify-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium transition disabled:cursor-not-allowed disabled:opacity-50",
        styles,
        className,
      )}
    >
      {loading && <Loader2 size={14} className="animate-spin" aria-hidden />}
      {children}
    </button>
  );
}

export function EmptyState({ icon, title, children }: { icon?: ReactNode; title: string; children?: ReactNode }) {
  return (
    <div className="flex flex-col items-center justify-center px-6 py-12 text-center">
      {icon && <div className="mb-3 text-ink-3">{icon}</div>}
      <p className="text-sm font-medium text-ink">{title}</p>
      {children && <div className="mt-1 max-w-md text-sm text-ink-3">{children}</div>}
    </div>
  );
}

export function ErrorNote({ error, what }: { error?: Error; what: string }) {
  if (!error) return null;
  return (
    <div className="mb-4 flex items-start gap-2 rounded-lg border border-critical/30 bg-critical/8 px-3 py-2 text-sm text-ink">
      <AlertTriangle size={16} className="mt-0.5 shrink-0 text-critical" aria-hidden />
      <span>
        Couldn&apos;t reach {what}: <span className="text-ink-2">{error.message}</span>
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
