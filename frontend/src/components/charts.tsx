"use client";

import { useEffect, useState } from "react";
import {
  Area,
  AreaChart,
  Bar,
  BarChart,
  CartesianGrid,
  Legend,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
  type TooltipContentProps,
} from "recharts";
import type { BaselineFeature } from "@/lib/api";

const TOKENS = ["--ink", "--ink-2", "--ink-3", "--line", "--line-strong", "--surface", "--series-1", "--series-2", "--series-3", "--accent"] as const;
type Theme = Record<(typeof TOKENS)[number], string>;

function readTheme(): Theme {
  if (typeof window === "undefined") return Object.fromEntries(TOKENS.map((t) => [t, "#888888"])) as Theme;
  const cs = getComputedStyle(document.documentElement);
  return Object.fromEntries(TOKENS.map((t) => [t, cs.getPropertyValue(t).trim()])) as Theme;
}

/**
 * Recharts writes colors into SVG attributes, where CSS variables aren't
 * reliable, so resolve the tokens to concrete colors and re-resolve whenever
 * the theme changes.
 */
export function useTheme(): Theme {
  const [theme, setTheme] = useState<Theme>(readTheme);
  useEffect(() => {
    const update = () => setTheme(readTheme());
    const obs = new MutationObserver(update);
    obs.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    mq.addEventListener("change", update);
    return () => {
      obs.disconnect();
      mq.removeEventListener("change", update);
    };
  }, []);
  return theme;
}

export function resolveColor(theme: Theme, color: string) {
  const m = /^var\((--[a-z0-9-]+)\)$/.exec(color);
  return m ? (theme[m[1] as keyof Theme] ?? color) : color;
}

function ChartTooltip({ active, payload, label, format }: Partial<TooltipContentProps<number, string>> & { format?: (v: number) => string }) {
  if (!active || !payload?.length) return null;
  return (
    <div className="rounded-lg border border-line bg-surface px-3 py-2 text-xs shadow-lg">
      {label !== undefined && <div className="mb-1 font-medium text-ink">{String(label)}</div>}
      {payload.map((p) => (
        <div key={String(p.dataKey)} className="flex items-center gap-2 text-ink-2">
          <span className="h-2 w-2 rounded-sm" style={{ background: p.color }} />
          <span className="flex-1">{p.name}</span>
          <span className="tabular font-medium text-ink">{format ? format(Number(p.value)) : String(p.value)}</span>
        </div>
      ))}
    </div>
  );
}

const axisProps = (t: Theme) => ({
  stroke: t["--line-strong"],
  tick: { fill: t["--ink-3"], fontSize: 11 },
  tickLine: false,
});

// ---------------------------------------------------------------------------

/** Grouped bars: one group per metric, one bar per series (model/version). */
export function MetricCompareChart({ series, height = 260 }: {
  series: { name: string; color: string; metrics: Record<string, number | undefined> }[];
  height?: number;
}) {
  const t = useTheme();
  const keys: [string, string][] = [["accuracy", "Accuracy"], ["precision", "Precision"], ["recall", "Recall"], ["f1", "F1"], ["roc_auc", "ROC-AUC"]];
  const data = keys.map(([k, label]) => {
    const row: Record<string, string | number> = { metric: label };
    for (const s of series) row[s.name] = s.metrics[k] ?? 0;
    return row;
  });
  return (
    <ResponsiveContainer width="100%" height={height}>
      <BarChart data={data} barGap={2} barCategoryGap="22%" margin={{ top: 8, right: 8, left: -12, bottom: 0 }}>
        <CartesianGrid vertical={false} stroke={t["--line"]} />
        <XAxis dataKey="metric" {...axisProps(t)} />
        <YAxis domain={[0, 1]} ticks={[0, 0.25, 0.5, 0.75, 1]} tickFormatter={(v) => v.toFixed(2)} {...axisProps(t)} axisLine={false} />
        <Tooltip cursor={{ fill: t["--line"], opacity: 0.4 }} content={<ChartTooltip format={(v) => v.toFixed(3)} />} />
        <Legend iconType="circle" iconSize={8} wrapperStyle={{ fontSize: 12, color: t["--ink-2"] }} />
        {series.map((s) => (
          <Bar key={s.name} dataKey={s.name} fill={resolveColor(t, s.color)} radius={[4, 4, 0, 0]} maxBarSize={28} />
        ))}
      </BarChart>
    </ResponsiveContainer>
  );
}

/** Horizontal bars of mean |SHAP| per feature, largest first. */
export function ShapChart({ data, color, top = 10 }: { data: { feature: string; mean_abs_shap: number }[]; color: string; top?: number }) {
  const t = useTheme();
  const rows = data.slice(0, top).map((d) => ({ feature: d.feature.replace(/_/g, " "), value: d.mean_abs_shap }));
  return (
    <ResponsiveContainer width="100%" height={Math.max(160, rows.length * 26 + 20)}>
      <BarChart data={rows} layout="vertical" margin={{ top: 0, right: 16, left: 8, bottom: 0 }} barCategoryGap={4}>
        <CartesianGrid horizontal={false} stroke={t["--line"]} />
        <XAxis type="number" {...axisProps(t)} tickFormatter={(v) => v.toFixed(2)} />
        <YAxis type="category" dataKey="feature" width={150} {...axisProps(t)} axisLine={false} />
        <Tooltip cursor={{ fill: t["--line"], opacity: 0.4 }} content={<ChartTooltip format={(v) => v.toFixed(4)} />} />
        <Bar dataKey="value" name="mean |SHAP|" fill={resolveColor(t, color)} radius={[0, 4, 4, 0]} maxBarSize={16} />
      </BarChart>
    </ResponsiveContainer>
  );
}

/** Training distribution of one feature: share of rows in each decile bin. */
export function BaselineChart({ feature, color }: { feature: BaselineFeature; color: string }) {
  const t = useTheme();
  const { edges, proportions } = feature.bins;
  const fmt = (v: number) => (Math.abs(v) >= 1000 ? Intl.NumberFormat(undefined, { notation: "compact" }).format(v) : String(+v.toFixed(2)));
  const rows = proportions.map((p, i) => {
    let label: string;
    if (feature.type === "binary") label = i === 0 ? "0 (no)" : "1 (yes)";
    else if (i === 0) label = `≤ ${fmt(edges[0])}`;
    else if (i === edges.length) label = `> ${fmt(edges[edges.length - 1])}`;
    else label = `${fmt(edges[i - 1])}–${fmt(edges[i])}`;
    return { bin: label, share: p };
  });
  const tilt = rows.length > 6;
  return (
    <ResponsiveContainer width="100%" height={220}>
      <BarChart data={rows} margin={{ top: 8, right: 8, left: -12, bottom: 0 }} barCategoryGap={2}>
        <CartesianGrid vertical={false} stroke={t["--line"]} />
        <XAxis dataKey="bin" {...axisProps(t)} interval={0} angle={tilt ? -30 : 0} textAnchor={tilt ? "end" : "middle"} height={tilt ? 48 : 24} />
        <YAxis tickFormatter={(v) => `${Math.round(v * 100)}%`} {...axisProps(t)} axisLine={false} />
        <Tooltip cursor={{ fill: t["--line"], opacity: 0.4 }} content={<ChartTooltip format={(v) => `${(v * 100).toFixed(1)}%`} />} />
        <Bar dataKey="share" name="share of training rows" fill={resolveColor(t, color)} radius={[4, 4, 0, 0]} />
      </BarChart>
    </ResponsiveContainer>
  );
}

/** 2x2 confusion matrix with a single-hue sequential fill. */
export function ConfusionMatrix({ matrix }: { matrix: number[][] }) {
  const [[tn, fp], [fn, tp]] = matrix;
  const max = Math.max(tn, fp, fn, tp);
  const cell = (v: number, label: string, good: boolean) => {
    const a = 0.12 + 0.75 * (v / max);
    return (
      <div className="rounded-lg p-3" style={{ background: `color-mix(in srgb, var(--series-1) ${Math.round(a * 100)}%, var(--surface))` }}>
        <div className={a > 0.55 ? "text-white" : "text-ink"}>
          <div className="tabular text-lg font-semibold">{v.toLocaleString()}</div>
          <div className="text-[11px] opacity-80">
            {label} {good ? "✓" : "✗"}
          </div>
        </div>
      </div>
    );
  };
  return (
    <div className="grid grid-cols-[auto_1fr_1fr] gap-1.5 text-xs">
      <div />
      <div className="pb-1 text-center text-ink-3">Predicted: repaid</div>
      <div className="pb-1 text-center text-ink-3">Predicted: default</div>
      <div className="flex items-center pr-2 text-right text-ink-3">Actually repaid</div>
      {cell(tn, "true negative", true)}
      {cell(fp, "false alarm", false)}
      <div className="flex items-center pr-2 text-right text-ink-3">Actually defaulted</div>
      {cell(fn, "missed default", false)}
      {cell(tp, "caught default", true)}
    </div>
  );
}

/** Rolling throughput (requests/sec), one area per model. */
export function ThroughputChart({ data, series }: { data: Record<string, number>[]; series: { key: string; color: string }[] }) {
  const t = useTheme();
  return (
    <ResponsiveContainer width="100%" height={240}>
      <AreaChart data={data} margin={{ top: 8, right: 8, left: -12, bottom: 0 }}>
        <CartesianGrid vertical={false} stroke={t["--line"]} />
        <XAxis
          dataKey="t"
          type="number"
          domain={["dataMin", "dataMax"]}
          tickFormatter={(v) => new Date(v).toLocaleTimeString(undefined, { minute: "2-digit", second: "2-digit" })}
          {...axisProps(t)}
        />
        <YAxis allowDecimals tickFormatter={(v) => (v >= 10 ? Math.round(v).toString() : v.toFixed(1))} {...axisProps(t)} axisLine={false} />
        <Tooltip
          content={<ChartTooltip format={(v) => `${v.toFixed(2)} req/s`} />}
          labelFormatter={(v) => new Date(Number(v)).toLocaleTimeString()}
          cursor={{ stroke: t["--line-strong"] }}
        />
        <Legend iconType="circle" iconSize={8} wrapperStyle={{ fontSize: 12, color: t["--ink-2"] }} />
        {series.map((s) => {
          const c = resolveColor(t, s.color);
          return (
            <Area key={s.key} type="monotone" dataKey={s.key} name={s.key} stroke={c} strokeWidth={2} fill={c} fillOpacity={0.12} isAnimationActive={false} dot={false} />
          );
        })}
      </AreaChart>
    </ResponsiveContainer>
  );
}

/** Latency distribution from the Prometheus histogram (non-cumulative counts). */
export function LatencyHistogram({ buckets, color }: { buckets: { le: number; count: number }[]; color: string }) {
  const t = useTheme();
  const rows: { bucket: string; count: number }[] = [];
  for (let i = 0; i < buckets.length; i++) {
    const b = buckets[i];
    const prevCount = i > 0 ? buckets[i - 1].count : 0;
    const prevLe = i > 0 ? buckets[i - 1].le : 0;
    const label = b.le === Infinity ? `> ${+(prevLe * 1000).toPrecision(3)} ms` : `≤ ${+(b.le * 1000).toPrecision(3)} ms`;
    rows.push({ bucket: label, count: b.count - prevCount });
  }
  return (
    <ResponsiveContainer width="100%" height={220}>
      <BarChart data={rows} margin={{ top: 8, right: 8, left: -12, bottom: 0 }} barCategoryGap={2}>
        <CartesianGrid vertical={false} stroke={t["--line"]} />
        <XAxis dataKey="bucket" {...axisProps(t)} interval={0} angle={-30} textAnchor="end" height={52} />
        <YAxis allowDecimals={false} {...axisProps(t)} axisLine={false} />
        <Tooltip cursor={{ fill: t["--line"], opacity: 0.4 }} content={<ChartTooltip format={(v) => `${v} requests`} />} />
        <Bar dataKey="count" name="requests" fill={resolveColor(t, color)} radius={[4, 4, 0, 0]} />
      </BarChart>
    </ResponsiveContainer>
  );
}
