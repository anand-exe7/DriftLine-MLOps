"use client";

import Link from "next/link";
import { ArrowRight, Bell, Boxes, Cpu, Database, HardDrive, Server } from "lucide-react";
import {
  Card,
  EmptyState,
  HealthPill,
  LiveBadge,
  PageHeader,
  Skeleton,
  StageBadge,
  StatTile,
  modelColor,
  type Health,
} from "@/components/ui";
import { go } from "@/lib/api";
import { compact, dateTime, ms, pct } from "@/lib/format";
import { useRegistry, useSystemHealth, useTraffic } from "@/lib/hooks";
import { histogramQuantile, mergeBuckets } from "@/lib/prom";
import { usePoll } from "@/lib/usePoll";

const h = (ok: boolean | undefined): Health => (ok === undefined ? "unknown" : ok ? "up" : "down");

export default function OverviewPage() {
  const health = useSystemHealth(5000);
  const registry = useRegistry(10000);
  const traffic = useTraffic(3000);
  const alerts = usePoll(() => go.alerts(5), 10000);

  const entries = registry.data ?? [];
  const versionCount = entries.reduce((n, e) => n + e.versions.length, 0);
  const prodCount = entries.filter((e) => e.production).length;

  const t = traffic.data ? [...traffic.data.values()] : [];
  const ok = t.reduce((n, x) => n + x.ok, 0);
  const errors = t.reduce((n, x) => n + x.errors, 0);
  const p95 = histogramQuantile(0.95, mergeBuckets(t));

  const services = [
    { name: "Go control plane", what: "Registry · proxy · drift", Icon: Server, state: h(health.data?.go), port: ":8080" },
    { name: "PostgreSQL", what: "Metadata store", Icon: Database, state: h(health.data?.postgres), port: ":5432" },
    { name: "MinIO", what: "Artifact storage", Icon: HardDrive, state: h(health.data?.minio), port: ":9000" },
    { name: "ml-service", what: "ONNX inference", Icon: Cpu, state: h(health.data?.ml), port: ":50051" },
  ];

  return (
    <>
      <PageHeader
        title="Overview"
        subtitle="Live state of services, models and traffic. See How it works for a walkthrough."
        actions={<LiveBadge updatedAt={health.updatedAt} error={health.error} intervalLabel="5s" />}
      />

      <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
        <StatTile label="Registered models" value={registry.data ? entries.length : "—"} hint="in the registry" />
        <StatTile label="Model versions" value={registry.data ? versionCount : "—"} hint="artifacts in MinIO" />
        <StatTile label="In production" value={registry.data ? prodCount : "—"} hint="one per model, max" />
        <StatTile label="Loaded in memory" value={health.data ? health.data.mlLoaded.length : "—"} hint="ONNX sessions warm" />
        <StatTile label="Predictions served" value={traffic.data ? compact(ok) : "—"} hint="since ml-service start" />
        <StatTile
          label="p95 inference"
          value={traffic.data ? ms(p95) : "—"}
          hint={traffic.data ? `${pct(ok + errors ? errors / (ok + errors) : 0)} rejected` : "latency"}
        />
      </div>

      <div className="mt-6 grid gap-6 lg:grid-cols-5">
        <Card title="Service health" subtitle="Polled every 5 seconds" className="lg:col-span-2" pad={false}>
          <ul className="divide-y divide-line">
            {services.map(({ name, what, Icon, state, port }) => (
              <li key={name} className="flex items-center gap-3 px-5 py-3.5">
                <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-surface-2 text-ink-2">
                  <Icon size={17} aria-hidden />
                </span>
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium text-ink">{name}</div>
                  <div className="truncate text-xs text-ink-3">
                    <span className="font-mono">{port}</span> · {what}
                  </div>
                </div>
                <HealthPill state={state} />
              </li>
            ))}
          </ul>
        </Card>

        <Card
          title="Production models"
          subtitle="The version each model currently serves, with its held-out test metrics"
          className="lg:col-span-3"
          actions={
            <Link href="/models" className="flex items-center gap-1 text-xs font-medium text-accent hover:underline">
              Registry <ArrowRight size={12} />
            </Link>
          }
          pad={false}
        >
          {!registry.data ? (
            <div className="space-y-3 p-5">
              <Skeleton className="h-12" />
              <Skeleton className="h-12" />
            </div>
          ) : entries.length === 0 ? (
            <EmptyState icon={<Boxes size={28} />} title="No models registered yet">
              Run <code className="font-mono">bash scripts/seed_registry.sh</code> to register the trained models.
            </EmptyState>
          ) : (
            <ul className="divide-y divide-line">
              {entries.map(({ model, production, versions }) => (
                <li key={model.id}>
                  <Link href={`/models/${model.name}`} className="flex flex-wrap items-center gap-x-6 gap-y-2 px-5 py-4 hover:bg-surface-2/60">
                    <div className="flex min-w-48 flex-1 items-center gap-3">
                      <span className="h-8 w-1 rounded-full" style={{ background: modelColor(model.name) }} />
                      <div>
                        <div className="font-mono text-sm font-medium text-ink">{model.name}</div>
                        <div className="text-xs text-ink-3">
                          {production ? (
                            <>
                              serving <b className="text-ink-2">{production.version}</b> · promoted {dateTime(production.promoted_at)}
                            </>
                          ) : (
                            `${versions.length} version${versions.length === 1 ? "" : "s"}, none in production`
                          )}
                        </div>
                      </div>
                    </div>
                    {production ? (
                      <div className="flex gap-6 text-right tabular">
                        {(
                          [
                            ["ROC-AUC", production.metrics.roc_auc],
                            ["Recall", production.metrics.recall],
                            ["F1", production.metrics.f1],
                          ] as const
                        ).map(([k, v]) => (
                          <div key={k}>
                            <div className="text-[11px] text-ink-3">{k}</div>
                            <div className="text-sm font-semibold text-ink">{v == null ? "—" : v.toFixed(3)}</div>
                          </div>
                        ))}
                        <StageBadge stage="production" />
                      </div>
                    ) : (
                      <span className="text-xs text-ink-3">Promote a version to serve it</span>
                    )}
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </Card>
      </div>

      <div className="mt-6">
        <Card
          title="Recent alerts"
          actions={
            <Link href="/alerts" className="flex items-center gap-1 text-xs font-medium text-accent hover:underline">
              All alerts <ArrowRight size={12} />
            </Link>
          }
          pad={false}
        >
          {alerts.data && alerts.data.length > 0 ? (
            <ul className="divide-y divide-line">
              {alerts.data.map((a) => (
                <li key={a.id} className="px-5 py-3">
                  <div className="truncate text-sm text-ink">{a.message}</div>
                  <div className="mt-0.5 text-xs text-ink-3">
                    {a.channel} · {a.status} · {dateTime(a.created_at)}
                  </div>
                </li>
              ))}
            </ul>
          ) : (
            <EmptyState icon={<Bell size={26} />} title={alerts.data ? "No alerts sent yet" : "Loading…"}>
              {alerts.data && "Alerts appear here when drift fires or you send a test alert."}
            </EmptyState>
          )}
        </Card>
      </div>
    </>
  );
}
