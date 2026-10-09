"use client";

import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import { Activity, ArrowRight, Flame, Snowflake, Waves } from "lucide-react";
import { LatencyHistogram, ThroughputChart } from "@/components/charts";
import {
  Card,
  EmptyState,
  ErrorNote,
  Explainer,
  LiveBadge,
  PageHeader,
  StatTile,
  modelColor,
} from "@/components/ui";
import { ms, pct } from "@/lib/format";
import { useRegistry, useSystemHealth, useTraffic } from "@/lib/hooks";
import { histogramQuantile, mergeBuckets, type ModelTraffic } from "@/lib/prom";

const WINDOW = 60; // samples kept (x 3s = 3 minutes)

export default function MonitoringPage() {
  const traffic = useTraffic(3000);
  const health = useSystemHealth(5000);
  const registry = useRegistry(15000);

  // Turn monotonically increasing counters into a requests/sec time series.
  const prev = useRef<{ t: number; totals: Map<string, number> } | null>(null);
  const [series, setSeries] = useState<Record<string, number>[]>([]);
  useEffect(() => {
    if (!traffic.data || !traffic.updatedAt) return;
    const t = traffic.updatedAt;
    const totals = new Map([...traffic.data.values()].map((m) => [m.key, m.ok + m.errors]));
    const p = prev.current;
    if (!p) {
      prev.current = { t, totals };
      return;
    }
    const dt = (t - p.t) / 1000;
    // Throttled timers can deliver scrapes milliseconds apart; a rate over a
    // tiny window is noise, so wait until at least a second has passed.
    if (dt < 1) return;
    prev.current = { t, totals };
    const point: Record<string, number> = { t };
    for (const [key, total] of totals) {
      // a counter that went down means ml-service restarted: count from zero
      const before = p.totals.get(key) ?? 0;
      point[key] = Math.max(0, total >= before ? total - before : total) / dt;
    }
    setSeries((s) => [...s, point].slice(-WINDOW));
  }, [traffic.data, traffic.updatedAt]);

  const all: ModelTraffic[] = traffic.data ? [...traffic.data.values()] : [];
  // ml-service labels requests for unknown models as "-" to keep metric cardinality bounded.
  const models = all.filter((m) => m.model !== "-").sort((a, b) => a.key.localeCompare(b.key));
  const unrouted = all.find((m) => m.model === "-")?.errors ?? 0;
  const totalOk = all.reduce((n, m) => n + m.ok, 0);
  const totalErr = all.reduce((n, m) => n + m.errors, 0);
  const merged = mergeBuckets(models);
  const last = series.at(-1);
  const rate = last ? Object.entries(last).filter(([k]) => k !== "t").reduce((n, [, v]) => n + v, 0) : 0;
  const [latencyKey, setLatencyKey] = useState<string>("all");
  const latencyBuckets = latencyKey === "all" ? merged : models.find((m) => m.key === latencyKey)?.buckets ?? [];

  const loaded = new Set(health.data?.mlLoaded ?? []);
  const production = (registry.data ?? []).filter((e) => e.production);

  return (
    <>
      <PageHeader
        title="Live Monitoring"
        subtitle="Real-time inference traffic, latency and model cache state, scraped from ml-service's Prometheus endpoint."
        actions={<LiveBadge updatedAt={traffic.updatedAt} error={traffic.error} intervalLabel="3s" />}
      />

      <Explainer title="Where do these numbers come from?">
        <p>
          ml-service exports Prometheus metrics at <code className="font-mono">/metrics</code>: a counter of predictions per
          model, version and outcome, and a histogram of inference time. This page scrapes it every 3 seconds. Throughput is
          the change in the counter between scrapes; p50/p95/p99 are estimated from the histogram buckets the same way
          Prometheus&apos; <code className="font-mono">histogram_quantile()</code> does.
        </p>
        <p>
          Counters reset when ml-service restarts. Send requests from the <Link href="/predict" className="font-medium text-accent hover:underline">Prediction Lab</Link>{" "}
          and watch them appear here.
        </p>
      </Explainer>
      <ErrorNote error={traffic.error} what="ml-service metrics" />

      <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
        <StatTile label="Throughput now" value={`${rate.toFixed(2)}/s`} hint="last 3s window" />
        <StatTile label="Predictions served" value={totalOk.toLocaleString()} hint="since ml-service start" />
        <StatTile label="Rejected" value={totalErr.toLocaleString()} hint={`${pct(totalOk + totalErr ? totalErr / (totalOk + totalErr) : 0)} of requests`} />
        <StatTile label="p50 latency" value={ms(histogramQuantile(0.5, merged))} hint="inference only" />
        <StatTile label="p95 latency" value={ms(histogramQuantile(0.95, merged))} hint="inference only" />
        <StatTile label="p99 latency" value={ms(histogramQuantile(0.99, merged))} hint="inference only" />
      </div>

      <div className="mt-6 grid gap-6 xl:grid-cols-3">
        <Card title="Requests per second" subtitle="Rolling 3-minute window, one area per model version" className="xl:col-span-2">
          {series.length < 2 ? (
            <EmptyState icon={<Activity size={26} />} title="Collecting samples…">The chart starts after two scrapes (about 6 seconds).</EmptyState>
          ) : (
            <ThroughputChart data={series} series={models.map((m) => ({ key: m.key, color: modelColor(m.model) }))} />
          )}
        </Card>

        <Card title="Model cache" subtitle="Which production versions are loaded in ml-service memory" pad={false}>
          {production.length === 0 ? (
            <EmptyState title="No production versions" />
          ) : (
            <ul className="divide-y divide-line">
              {production.map((e) => {
                const key = `${e.model.name}/${e.production!.version}`;
                const warm = loaded.has(key);
                return (
                  <li key={key} className="flex items-center gap-3 px-5 py-3">
                    <span className="h-6 w-1 rounded-full" style={{ background: modelColor(e.model.name) }} />
                    <div className="min-w-0 flex-1">
                      <div className="truncate font-mono text-sm text-ink">{e.model.name}</div>
                      <div className="text-xs text-ink-3">production · {e.production!.version}</div>
                    </div>
                    <span className={warm ? "inline-flex items-center gap-1 text-xs font-medium text-serious" : "inline-flex items-center gap-1 text-xs text-ink-3"}>
                      {warm ? <Flame size={13} /> : <Snowflake size={13} />}
                      {warm ? "Warm" : "Cold"}
                    </span>
                  </li>
                );
              })}
            </ul>
          )}
          <p className="border-t border-line px-5 py-3 text-xs leading-relaxed text-ink-3">
            <b>Warm</b> = an ONNX session is already in memory. A <b>cold</b> version is fetched from MinIO and loaded on its
            first request, so that one request is slower.
          </p>
        </Card>
      </div>

      <div className="mt-6 grid gap-6 xl:grid-cols-3">
        <Card title="Per-model traffic" subtitle="Cumulative since ml-service start" className="xl:col-span-2" pad={false}>
          {models.length === 0 ? (
            <EmptyState icon={<Activity size={26} />} title="No traffic yet">
              Open the <Link href="/predict" className="font-medium text-accent hover:underline">Prediction Lab</Link> to send requests.
            </EmptyState>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full min-w-[620px] text-sm">
                <thead>
                  <tr className="border-b border-line text-left text-xs text-ink-3">
                    <th className="px-5 py-2.5 font-medium">Model / version</th>
                    <th className="px-3 py-2.5 text-right font-medium">OK</th>
                    <th className="px-3 py-2.5 text-right font-medium">Rejected</th>
                    <th className="px-3 py-2.5 text-right font-medium">Mean</th>
                    <th className="px-3 py-2.5 text-right font-medium">p95</th>
                    <th className="px-5 py-2.5 text-right font-medium">Now</th>
                  </tr>
                </thead>
                <tbody className="tabular divide-y divide-line">
                  {models.map((m) => (
                    <tr key={m.key}>
                      <td className="px-5 py-2.5">
                        <span className="flex items-center gap-2 font-mono text-ink">
                          <span className="h-2.5 w-2.5 rounded-sm" style={{ background: modelColor(m.model) }} />
                          {m.key}
                        </span>
                      </td>
                      <td className="px-3 py-2.5 text-right text-ink">{m.ok.toLocaleString()}</td>
                      <td className="px-3 py-2.5 text-right text-ink-2">{m.errors.toLocaleString()}</td>
                      <td className="px-3 py-2.5 text-right text-ink">{ms(m.latencyCount ? m.latencySum / m.latencyCount : undefined)}</td>
                      <td className="px-3 py-2.5 text-right text-ink">{ms(histogramQuantile(0.95, m.buckets))}</td>
                      <td className="px-5 py-2.5 text-right text-ink-2">{((last?.[m.key] as number) ?? 0).toFixed(2)}/s</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          {unrouted > 0 && (
            <p className="border-t border-line px-5 py-3 text-xs text-ink-3">
              Plus <b className="text-ink-2">{unrouted.toLocaleString()}</b> request{unrouted === 1 ? "" : "s"} for models that don&apos;t exist,
              grouped under one label so arbitrary names can&apos;t blow up metric cardinality.
            </p>
          )}
        </Card>

        <Card
          title="Latency distribution"
          subtitle="Requests per histogram bucket"
          actions={
            <select value={latencyKey} onChange={(e) => setLatencyKey(e.target.value)} className="rounded-md border border-line-strong bg-surface px-2 py-1 text-xs text-ink">
              <option value="all">All models</option>
              {models.filter((m) => m.buckets.length).map((m) => (
                <option key={m.key} value={m.key}>{m.key}</option>
              ))}
            </select>
          }
        >
          {latencyBuckets.length === 0 || latencyBuckets.at(-1)!.count === 0 ? (
            <EmptyState title="No latency samples yet" />
          ) : (
            <LatencyHistogram buckets={latencyBuckets} color={latencyKey === "all" ? "var(--accent)" : modelColor(latencyKey)} />
          )}
        </Card>
      </div>

      <Card title="Data drift" subtitle="Comparing live inputs against each version's training baseline" className="mt-6">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-center">
          <span className="flex h-11 w-11 shrink-0 items-center justify-center rounded-xl bg-accent-soft text-accent">
            <Waves size={20} />
          </span>
          <div className="flex-1 text-sm leading-relaxed text-ink-2">
            The baseline side is ready: every registered version carries its training distribution (decile bins per feature),
            and the database has <code className="font-mono">prediction_logs</code> and <code className="font-mono">drift_reports</code> tables
            waiting. The <b>drift scheduler</b> that logs live requests and computes PSI per feature is the next piece being built;
            when it flags drift, it calls the alert service that is already running.
          </div>
          <Link href="/models" className="inline-flex items-center gap-1 whitespace-nowrap text-sm font-medium text-accent hover:underline">
            View baselines <ArrowRight size={14} />
          </Link>
        </div>
      </Card>
    </>
  );
}
