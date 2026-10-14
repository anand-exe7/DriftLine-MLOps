"use client";

import { useMemo, useState } from "react";
import { AlertTriangle, CheckCircle2, Play, Waves } from "lucide-react";
import { FeaturePSIChart, PSITimeline, psiLevel } from "@/components/charts";
import {
  Button,
  Card,
  EmptyState,
  ErrorNote,
  Explainer,
  LiveBadge,
  PageHeader,
  StatTile,
  cx,
} from "@/components/ui";
import { go, type DriftReport } from "@/lib/api";
import { ago, dateTime } from "@/lib/format";
import { useNow, usePoll } from "@/lib/usePoll";

// Distinct colors for version lines: categorical slots in fixed order.
const SLOTS = ["var(--series-1)", "var(--series-2)", "var(--series-3)", "#eda100", "#e87ba4"];

export default function DriftPage() {
  const reports = usePoll(() => go.driftReports("", 300), 10000);
  const config = usePoll(() => go.driftConfig(), 60000);
  const now = useNow(5000);
  const [running, setRunning] = useState(false);
  const [runMsg, setRunMsg] = useState<string>();

  const list = useMemo(() => reports.data ?? [], [reports.data]);

  // Latest report per model/version.
  const latest = useMemo(() => {
    const m = new Map<string, DriftReport>();
    for (const r of list) {
      const k = `${r.model_name}/${r.version}`;
      if (!m.has(k)) m.set(k, r);
    }
    return [...m.values()];
  }, [list]);

  const timeline = useMemo(() => {
    const keys = [...new Set(list.map((r) => `${r.model_name}/${r.version}`))];
    const points = [...list].reverse().map((r) => ({ t: Date.parse(r.window_end), [`${r.model_name}/${r.version}`]: r.overall_psi }));
    return { keys, points };
  }, [list]);

  const [selected, setSelected] = useState<string>();
  const current = latest.find((r) => `${r.model_name}/${r.version}` === selected) ?? latest[0];
  const drifting = latest.filter((r) => r.drift_detected).length;

  const runNow = async () => {
    setRunning(true);
    setRunMsg(undefined);
    try {
      const res = await go.runDrift();
      setRunMsg(res.length ? `Checked ${res.length} version${res.length > 1 ? "s" : ""}: ${res.filter((r) => r.drift_detected).length} drifting` : `No version had enough traffic in the window (minimum ${config.data?.min_samples ?? 100} predictions)`);
      reports.refresh();
    } catch (e) {
      setRunMsg(e instanceof Error ? e.message : String(e));
    } finally {
      setRunning(false);
    }
  };

  return (
    <>
      <PageHeader
        title="Data drift"
        subtitle="Are the applications the models see today still like the ones they were trained on?"
        actions={
          <>
            <LiveBadge updatedAt={reports.updatedAt} error={reports.error} intervalLabel="10s" />
            <Button onClick={runNow} loading={running}>
              <Play size={14} /> Run check now
            </Button>
          </>
        }
      />

      <Explainer title="How drift is measured">
        <p>
          Every logged prediction stores its inputs. On a schedule, the drift engine takes the recent window of live inputs for
          each serving version and buckets every feature using that version&apos;s <b>training deciles</b>. The{" "}
          <b>Population Stability Index</b> sums how much each bucket&apos;s share moved: PSI = Σ (live − train) · ln(live / train).
        </p>
        <p>
          A feature at PSI ≥ {config.data?.feature_psi_trigger ?? 0.25}, or an average across features ≥{" "}
          {config.data?.overall_psi_trigger ?? 0.1}, counts as drift. Continuous features also get a binned{" "}
          <b>Kolmogorov–Smirnov</b> test (largest gap between the two cumulative distributions, with a p-value). Drift raises an
          alert; drift that affects a canary but not the primary rolls the canary back.
        </p>
      </Explainer>
      <ErrorNote error={reports.error} what="the drift engine" />
      {runMsg && <p className="mb-4 rounded-lg border border-line bg-surface px-3 py-2 text-sm text-ink-2">{runMsg}</p>}

      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        <StatTile label="Versions monitored" value={reports.data ? latest.length : "—"} hint="with recent traffic" />
        <StatTile
          label="Drifting now"
          value={
            reports.data ? (
              <span className={cx("inline-flex items-center gap-2", drifting ? "text-critical" : "text-good-ink")}>
                {drifting ? <AlertTriangle size={20} /> : <CheckCircle2 size={20} />}
                {drifting}
              </span>
            ) : "—"
          }
          hint="latest check per version"
        />
        <StatTile label="Check interval" value={config.data?.interval ?? "—"} hint={`window ${config.data?.window ?? "—"}`} />
        <StatTile label="Last check" value={list[0] ? ago(list[0].created_at, now) : "never"} hint={`min ${config.data?.min_samples ?? "—"} predictions`} />
      </div>

      {latest.length === 0 ? (
        <Card className="mt-6">
          <EmptyState icon={<Waves size={28} />} title="No drift reports yet">
            Reports appear once a version has served at least {config.data?.min_samples ?? 100} predictions within the window.
            Generate traffic with <code className="font-mono">python scripts/simulate_traffic.py</code> (add{" "}
            <code className="font-mono">--mode drift</code> to see drift being detected).
          </EmptyState>
        </Card>
      ) : (
        <>
          <div className="mt-6 grid gap-6 xl:grid-cols-5">
            <Card title="Latest check per version" className="xl:col-span-2" pad={false}>
              <ul className="divide-y divide-line">
                {latest.map((r) => {
                  const k = `${r.model_name}/${r.version}`;
                  const lvl = psiLevel(r.overall_psi);
                  const isSel = current && k === `${current.model_name}/${current.version}`;
                  const worst = Object.entries(r.feature_scores).sort((a, b) => b[1].psi - a[1].psi)[0];
                  return (
                    <li key={k}>
                      <button onClick={() => setSelected(k)} className={cx("flex w-full items-center gap-3 px-5 py-3 text-left", isSel ? "bg-accent-soft/50" : "hover:bg-surface-2/60")}>
                        {r.drift_detected ? <AlertTriangle size={18} className="shrink-0 text-critical" aria-label="drift" /> : <CheckCircle2 size={18} className="shrink-0 text-good-ink" aria-label="stable" />}
                        <div className="min-w-0 flex-1">
                          <div className="font-mono text-sm text-ink">{k}</div>
                          <div className="truncate text-xs text-ink-3">
                            n={r.sample_size.toLocaleString()} · worst: {worst ? `${worst[0]} ${worst[1].psi.toFixed(2)}` : "—"} · {ago(r.created_at, now)}
                          </div>
                        </div>
                        <div className="text-right">
                          <div className="tabular text-sm font-semibold text-ink">{r.overall_psi.toFixed(3)}</div>
                          <div className="text-[11px]" style={{ color: lvl.color }}>{r.drift_detected ? "drift" : lvl.label}</div>
                        </div>
                      </button>
                    </li>
                  );
                })}
              </ul>
            </Card>

            {current && (
              <Card
                title={<span>Feature PSI · <span className="font-mono">{current.model_name}/{current.version}</span></span>}
                subtitle={`${current.sample_size.toLocaleString()} live predictions between ${dateTime(current.window_start)} and ${dateTime(current.window_end)}`}
                className="xl:col-span-3"
              >
                <FeaturePSIChart scores={current.feature_scores} />
                <div className="mt-3 flex flex-wrap gap-4 text-[11px] text-ink-3">
                  {[["stable", "< 0.1", "#0ca30c"], ["moderate", "0.1–0.25", "#ec835a"], ["significant", "≥ 0.25", "#d03b3b"]].map(([l, r, c]) => (
                    <span key={l} className="flex items-center gap-1.5"><span className="h-2.5 w-2.5 rounded-sm" style={{ background: c }} />{l} ({r})</span>
                  ))}
                </div>
              </Card>
            )}
          </div>

          {current && <KSTable report={current} />}

          <Card title="Overall PSI over time" subtitle="Mean PSI across features at each check" className="mt-6">
            <PSITimeline data={timeline.points} series={timeline.keys.map((k, i) => ({ key: k, color: SLOTS[i % SLOTS.length] }))} />
          </Card>
        </>
      )}
    </>
  );
}

function KSTable({ report }: { report: DriftReport }) {
  const rows = Object.entries(report.feature_scores)
    .filter(([, s]) => s.ks_stat != null)
    .sort((a, b) => (b[1].ks_stat ?? 0) - (a[1].ks_stat ?? 0));
  if (!rows.length) return null;
  return (
    <Card title="Kolmogorov–Smirnov (continuous features)" subtitle="Largest gap between live and training CDFs; p < 0.05 means the shift is unlikely to be chance" className="mt-6" pad={false}>
      <div className="overflow-x-auto">
        <table className="w-full min-w-[560px] text-sm">
          <thead>
            <tr className="border-b border-line text-left text-xs text-ink-3">
              <th className="px-5 py-2.5 font-medium">Feature</th>
              <th className="px-3 py-2.5 text-right font-medium">PSI</th>
              <th className="px-3 py-2.5 text-right font-medium">KS statistic</th>
              <th className="px-3 py-2.5 text-right font-medium">p-value</th>
              <th className="px-5 py-2.5 font-medium">Verdict</th>
            </tr>
          </thead>
          <tbody className="tabular divide-y divide-line">
            {rows.map(([f, s]) => {
              const sig = (s.ks_p ?? 1) < 0.05;
              return (
                <tr key={f}>
                  <td className="px-5 py-2 font-mono text-ink">{f}</td>
                  <td className="px-3 py-2 text-right text-ink">{s.psi.toFixed(3)}</td>
                  <td className="px-3 py-2 text-right text-ink">{s.ks_stat!.toFixed(3)}</td>
                  <td className="px-3 py-2 text-right text-ink">{(s.ks_p ?? 1) < 0.0001 ? "< 0.0001" : s.ks_p!.toFixed(4)}</td>
                  <td className={cx("px-5 py-2 text-xs font-medium", sig ? "text-critical" : "text-good-ink")}>
                    {sig ? "shifted" : "consistent"}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </Card>
  );
}
