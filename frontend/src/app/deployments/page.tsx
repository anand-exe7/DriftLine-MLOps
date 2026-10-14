"use client";

import Link from "next/link";
import { useMemo, useState } from "react";
import { ArrowUpCircle, History, Rocket, RotateCcw, Square } from "lucide-react";
import {
  Button,
  Card,
  EmptyState,
  ErrorNote,
  Explainer,
  LiveBadge,
  PageHeader,
  Skeleton,
  cx,
} from "@/components/ui";
import { go, type Deployment, type VersionStats } from "@/lib/api";
import { dateTime, pct } from "@/lib/format";
import { useRegistry, type RegistryEntry } from "@/lib/hooks";
import { usePoll } from "@/lib/usePoll";

const STATUS_STYLE: Record<Deployment["status"], string> = {
  running: "bg-good/12 text-good-ink ring-good/30",
  pending: "bg-accent-soft text-accent ring-accent/30",
  stopped: "bg-surface-2 text-ink-3 ring-line",
  failed: "bg-critical/10 text-critical ring-critical/30",
  rolled_back: "bg-serious/12 text-serious ring-serious/30",
};

function StatusBadge({ status }: { status: Deployment["status"] }) {
  return (
    <span className={cx("inline-flex rounded-full px-2 py-0.5 text-[11px] font-medium ring-1 ring-inset", STATUS_STYLE[status])}>
      {status.replace("_", " ")}
    </span>
  );
}

export default function DeploymentsPage() {
  const registry = useRegistry(10000);
  const deployments = usePoll(() => go.deployments(), 4000);
  const [actionError, setActionError] = useState<string>();
  const [busy, setBusy] = useState<string>();

  const act = async (key: string, fn: () => Promise<unknown>) => {
    setBusy(key);
    setActionError(undefined);
    try {
      await fn();
      deployments.refresh();
      registry.refresh();
    } catch (e) {
      setActionError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(undefined);
    }
  };

  const all = useMemo(() => deployments.data ?? [], [deployments.data]);
  const models = (registry.data ?? []).map((e) => e.model.name);

  return (
    <>
      <PageHeader
        title="Deployments"
        subtitle="Which model versions serve live traffic, how traffic is split between them, and how they are performing right now."
        actions={<LiveBadge updatedAt={deployments.updatedAt} error={deployments.error} intervalLabel="4s" />}
      />

      <Explainer title="How canary releases work">
        <p>
          Each model has one <b>primary</b> deployment. To release a new version safely, deploy it as a <b>canary</b> with a
          small share of traffic (say 10%). The Go prediction proxy splits requests exactly by those weights, and every request
          is logged with the version that served it.
        </p>
        <p>
          The <b>rollback manager</b> checks every canary every 30 seconds. If its error rate or its share of predicted
          defaults diverges from the primary&apos;s on the same traffic, it rolls the canary back automatically and alerts
          the team. When the canary looks healthy, <b>promote</b> it: it becomes the primary at 100% and the registry marks
          it production. <b>Roll back</b> on a model with no canary redeploys the previous primary version.
        </p>
      </Explainer>
      <ErrorNote error={deployments.error} what="the deployment manager" />
      {actionError && <ErrorNote error={new Error(actionError)} what="that action" />}

      <DeployForm registry={registry.data ?? []} onDone={() => { deployments.refresh(); registry.refresh(); }} />

      {!deployments.data && !deployments.error ? (
        <Skeleton className="h-64" />
      ) : models.length === 0 ? (
        <Card>
          <EmptyState icon={<Rocket size={28} />} title="No models registered">
            Register a model in the <Link href="/models" className="text-accent hover:underline">Model Registry</Link> first.
          </EmptyState>
        </Card>
      ) : (
        <div className="space-y-6">
          {models.map((m) => (
            <ModelDeployments
              key={m}
              model={m}
              running={all
                .filter((d) => d.model_name === m && d.status === "running")
                .sort((a, b) => (a.role === b.role ? 0 : a.role === "primary" ? -1 : 1))}
              production={(registry.data ?? []).find((e) => e.model.name === m)?.production?.version}
              busy={busy}
              act={act}
            />
          ))}
        </div>
      )}

      <Card title="History" subtitle="Every deployment, newest first" className="mt-6" pad={false}>
        {all.length === 0 ? (
          <EmptyState icon={<History size={26} />} title="No deployments yet" />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[720px] text-sm">
              <thead>
                <tr className="border-b border-line text-left text-xs text-ink-3">
                  <th className="px-5 py-2.5 font-medium">Model / version</th>
                  <th className="px-3 py-2.5 font-medium">Role</th>
                  <th className="px-3 py-2.5 font-medium">Status</th>
                  <th className="px-3 py-2.5 text-right font-medium">Traffic</th>
                  <th className="px-3 py-2.5 font-medium">Reason</th>
                  <th className="px-5 py-2.5 font-medium">Started</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line">
                {all.slice(0, 40).map((d) => (
                  <tr key={d.id}>
                    <td className="px-5 py-2.5 font-mono text-ink">
                      {d.model_name}/<b>{d.version}</b>
                    </td>
                    <td className="px-3 py-2.5 capitalize text-ink-2">{d.role}</td>
                    <td className="px-3 py-2.5"><StatusBadge status={d.status} /></td>
                    <td className="tabular px-3 py-2.5 text-right text-ink">{d.status === "running" ? `${d.traffic_percent}%` : "—"}</td>
                    <td className="max-w-xs truncate px-3 py-2.5 text-xs text-ink-2" title={d.status_reason}>{d.status_reason || "—"}</td>
                    <td className="px-5 py-2.5 text-xs text-ink-3">{dateTime(d.started_at ?? d.created_at)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>
    </>
  );
}

function ModelDeployments({ model, running, production, busy, act }: {
  model: string;
  running: Deployment[];
  production?: string;
  busy?: string;
  act: (key: string, fn: () => Promise<unknown>) => void;
}) {
  const stats = usePoll(() => go.stats(model, "15m"), 5000, [model]);
  const primary = running.find((d) => d.role === "primary");
  const canary = running.find((d) => d.role === "canary");
  const [pctDraft, setPctDraft] = useState<number>();
  const canaryPct = pctDraft ?? canary?.traffic_percent ?? 10;
  const byVersion = new Map((stats.data?.versions ?? []).map((s) => [s.version, s]));

  return (
    <Card
      title={<span className="font-mono">{model}</span>}
      subtitle={
        running.length
          ? `${running.length} running deployment${running.length > 1 ? "s" : ""}`
          : production
            ? `No deployment: the proxy serves the registry's production version (${production})`
            : "Not serving: deploy a version below"
      }
      actions={
        <Button
          variant="secondary"
          className="px-2.5 py-1 text-xs"
          loading={busy === `rb:${model}`}
          disabled={!primary}
          onClick={() => act(`rb:${model}`, () => go.rollback(model, "manual rollback from dashboard"))}
        >
          <RotateCcw size={13} /> Roll back
        </Button>
      }
    >
      {running.length > 0 && (
        <>
          {/* traffic split bar */}
          <div className="mb-1 flex h-9 overflow-hidden rounded-lg ring-1 ring-line">
            {running.map((d) => (
              <div
                key={d.id}
                className="flex items-center justify-center text-xs font-medium text-white transition-all duration-500"
                style={{ width: `${d.traffic_percent}%`, background: d.role === "canary" ? "var(--series-2)" : "var(--series-1)" }}
                title={`${d.version} ${d.traffic_percent}%`}
              >
                {d.traffic_percent >= 8 && `${d.version} · ${d.traffic_percent}%`}
              </div>
            ))}
          </div>
          <div className="mb-5 flex gap-4 text-[11px] text-ink-3">
            <span className="flex items-center gap-1.5"><span className="h-2 w-2 rounded-sm" style={{ background: "var(--series-1)" }} />primary</span>
            <span className="flex items-center gap-1.5"><span className="h-2 w-2 rounded-sm" style={{ background: "var(--series-2)" }} />canary</span>
          </div>

          <div className="grid gap-4 md:grid-cols-2">
            {[primary, canary].filter(Boolean).map((d) => (
              <VersionPanel key={d!.id} d={d!} s={byVersion.get(d!.version)} />
            ))}
          </div>

          {canary && (
            <div className="mt-5 flex flex-col gap-4 rounded-lg border border-line p-4 lg:flex-row lg:items-center">
              <div className="flex-1">
                <div className="flex items-baseline justify-between text-sm">
                  <label htmlFor={`pct-${model}`} className="font-medium text-ink">Canary traffic</label>
                  <span className="tabular text-ink-2">{canaryPct}% canary · {100 - canaryPct}% primary</span>
                </div>
                <input
                  id={`pct-${model}`}
                  type="range"
                  min={1}
                  max={99}
                  value={canaryPct}
                  onChange={(e) => setPctDraft(Number(e.target.value))}
                  className="mt-2 w-full accent-[var(--series-2)]"
                />
              </div>
              <div className="flex flex-wrap gap-2">
                <Button
                  variant="secondary"
                  disabled={pctDraft === undefined || pctDraft === canary.traffic_percent}
                  loading={busy === `pct:${canary.id}`}
                  onClick={() => act(`pct:${canary.id}`, async () => { await go.setTraffic(canary.id, canaryPct); setPctDraft(undefined); })}
                >
                  Apply split
                </Button>
                <Button loading={busy === `pro:${canary.id}`} onClick={() => act(`pro:${canary.id}`, () => go.promoteDeployment(canary.id))}>
                  <ArrowUpCircle size={14} /> Promote canary
                </Button>
                <Button
                  variant="danger"
                  loading={busy === `stop:${canary.id}`}
                  onClick={() => act(`stop:${canary.id}`, () => go.rollback(model, "canary rolled back from dashboard"))}
                >
                  <Square size={13} /> Stop canary
                </Button>
              </div>
            </div>
          )}
        </>
      )}
      {running.length === 0 && (
        <p className="text-sm text-ink-3">
          Use the form above to deploy a version at 100%. After that you can add a canary next to it.
        </p>
      )}
    </Card>
  );
}

function VersionPanel({ d, s }: { d: Deployment; s?: VersionStats }) {
  const errRate = s && s.requests ? s.errors / s.requests : undefined;
  return (
    <div className="rounded-lg border border-line p-4">
      <div className="flex items-center justify-between">
        <span className="flex items-center gap-2 font-mono text-sm font-medium text-ink">
          <span className="h-3 w-3 rounded-sm" style={{ background: d.role === "canary" ? "var(--series-2)" : "var(--series-1)" }} />
          {d.version}
        </span>
        <span className="text-xs capitalize text-ink-3">{d.role} · since {dateTime(d.started_at)}</span>
      </div>
      <dl className="mt-3 grid grid-cols-4 gap-2 text-center">
        {(
          [
            ["Requests", s ? s.requests.toLocaleString() : "0"],
            ["Errors", errRate === undefined ? "—" : pct(errRate)],
            ["Flagged", s && s.requests ? pct(s.default_rate) : "—"],
            ["Latency", s && s.requests ? (s.avg_latency_ms < 1 ? "<1 ms" : `${s.avg_latency_ms.toFixed(1)} ms`) : "—"],
          ] as const
        ).map(([k, v]) => (
          <div key={k} className="rounded-md bg-surface-2 px-1 py-2">
            <dt className="text-[10px] text-ink-3">{k}</dt>
            <dd className="tabular text-sm font-semibold text-ink">{v}</dd>
          </div>
        ))}
      </dl>
      <p className="mt-2 text-[11px] text-ink-3">Last 15 minutes of logged traffic · &ldquo;Flagged&rdquo; = share predicted to default</p>
    </div>
  );
}

function DeployForm({ registry, onDone }: { registry: RegistryEntry[]; onDone: () => void }) {
  const [model, setModel] = useState("");
  const [version, setVersion] = useState("");
  const [traffic, setTraffic] = useState(100);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<{ ok: boolean; text: string }>();

  const entry = registry.find((e) => e.model.name === model) ?? registry[0];
  const versions = (entry?.versions ?? []).filter((v) => v.stage !== "archived");
  const chosenModel = entry?.model.name ?? "";
  const chosenVersion = versions.find((v) => v.version === version)?.version ?? versions[0]?.version ?? "";

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setMsg(undefined);
    try {
      const d = await go.deploy(chosenModel, chosenVersion, traffic);
      setMsg({ ok: true, text: `${d.model_name}/${d.version} is now ${d.role} at ${d.traffic_percent}%` });
      onDone();
    } catch (err) {
      setMsg({ ok: false, text: err instanceof Error ? err.message : String(err) });
    } finally {
      setBusy(false);
    }
  };

  if (registry.length === 0) return null;
  return (
    <Card title="Deploy a version" subtitle="100% replaces the primary (blue/green). Less than 100% starts a canary next to the primary." className="mb-6">
      <form onSubmit={submit} className="grid gap-4 sm:grid-cols-2 lg:grid-cols-[1fr_1fr_1.4fr_auto] lg:items-end">
        <label className="text-sm">
          <span className="mb-1 block text-xs font-medium text-ink-2">Model</span>
          <select value={chosenModel} onChange={(e) => { setModel(e.target.value); setVersion(""); }} className="w-full rounded-lg border border-line-strong bg-surface px-2.5 py-2 font-mono text-sm text-ink">
            {registry.map((e) => <option key={e.model.id} value={e.model.name}>{e.model.name}</option>)}
          </select>
        </label>
        <label className="text-sm">
          <span className="mb-1 block text-xs font-medium text-ink-2">Version</span>
          <select value={chosenVersion} onChange={(e) => setVersion(e.target.value)} className="w-full rounded-lg border border-line-strong bg-surface px-2.5 py-2 font-mono text-sm text-ink">
            {versions.map((v) => <option key={v.id} value={v.version}>{v.version} · {v.stage}</option>)}
          </select>
        </label>
        <div className="text-sm">
          <div className="mb-1 flex justify-between text-xs font-medium text-ink-2">
            <span>Traffic</span>
            <span className="tabular">{traffic === 100 ? "100% (primary)" : `${traffic}% (canary)`}</span>
          </div>
          <input type="range" min={1} max={100} value={traffic} onChange={(e) => setTraffic(Number(e.target.value))} className="w-full accent-[var(--accent)]" />
        </div>
        <Button type="submit" loading={busy} disabled={!chosenVersion}>
          <Rocket size={14} /> Deploy
        </Button>
      </form>
      {msg && <p className={cx("mt-3 text-sm", msg.ok ? "text-good-ink" : "text-critical")}>{msg.text}</p>}
      <p className="mt-3 text-xs text-ink-3">
        The version is loaded into ml-service before it receives any traffic; if loading fails, nothing changes.
      </p>
    </Card>
  );
}
