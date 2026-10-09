"use client";

import Link from "next/link";
import { useState } from "react";
import { ArrowRight, Boxes, Plus } from "lucide-react";
import { MetricCompareChart } from "@/components/charts";
import {
  Button,
  Card,
  EmptyState,
  ErrorNote,
  Explainer,
  LiveBadge,
  PageHeader,
  Skeleton,
  StageBadge,
  modelColor,
} from "@/components/ui";
import { go } from "@/lib/api";
import { ago } from "@/lib/format";
import { useRegistry } from "@/lib/hooks";
import { useNow } from "@/lib/usePoll";

export default function ModelsPage() {
  const registry = useRegistry(10000);
  const now = useNow(10000);
  const [showCreate, setShowCreate] = useState(false);
  const entries = registry.data ?? [];
  const compared = entries.filter((e) => e.production && Object.keys(e.production.metrics ?? {}).length > 0);

  return (
    <>
      <PageHeader
        title="Model Registry"
        subtitle="Every trained model and each of its versions: the artifact, its test metrics, and where it is in the release lifecycle."
        actions={
          <>
            <LiveBadge updatedAt={registry.updatedAt} error={registry.error} intervalLabel="10s" />
            <Button onClick={() => setShowCreate((s) => !s)}>
              <Plus size={15} /> New model
            </Button>
          </>
        }
      />

      <Explainer title="How the registry works">
        <p>
          A <b>model</b> is a named prediction task (e.g. <code className="font-mono">loan_default_xgb</code>). Each time it is
          retrained you upload a new <b>version</b>: the <code className="font-mono">model.onnx</code> file plus a{" "}
          <code className="font-mono">feature_schema.json</code> listing the inputs in order. The Go service streams the file
          to MinIO, records a SHA-256 checksum, and stores the metadata in Postgres.
        </p>
        <p>
          Versions move through stages: <StageBadge stage="registered" /> → <StageBadge stage="staging" /> →{" "}
          <StageBadge stage="production" />. Only <b>one</b> version per model can be in production; promoting a new one
          automatically archives the old one (the database enforces this). That is the hook rollback will use.
        </p>
      </Explainer>

      {showCreate && <CreateModel onDone={() => { setShowCreate(false); registry.refresh(); }} />}
      <ErrorNote error={registry.error} what="the Go control plane" />

      {compared.length > 1 && (
        <Card
          title="Model comparison: production versions"
          subtitle="Evaluated on the same held-out test set (51,070 loans). Higher is better for every metric."
          className="mb-6"
        >
          <MetricCompareChart
            series={compared.map((e) => ({
              name: `${e.model.name} · ${e.production!.version}`,
              color: modelColor(e.model.name),
              metrics: e.production!.metrics as Record<string, number | undefined>,
            }))}
          />
          <p className="mt-3 text-xs text-ink-3">
            Both models use class weighting, so they trade some accuracy for catching far more real defaults (recall). ROC-AUC is
            the threshold-free measure of how well each one ranks risky applicants above safe ones.
          </p>
        </Card>
      )}

      {!registry.data && !registry.error ? (
        <div className="grid gap-4 md:grid-cols-2">
          <Skeleton className="h-40" />
          <Skeleton className="h-40" />
        </div>
      ) : entries.length === 0 ? (
        <Card>
          <EmptyState icon={<Boxes size={28} />} title="No models yet">
            Create one above, or run <code className="font-mono">bash scripts/seed_registry.sh</code> to register the trained loan-default models.
          </EmptyState>
        </Card>
      ) : (
        <div className="grid gap-4 md:grid-cols-2">
          {entries.map(({ model, versions, production }) => {
            const latest = versions[0];
            const counts = versions.reduce<Record<string, number>>((m, v) => ((m[v.stage] = (m[v.stage] ?? 0) + 1), m), {});
            return (
              <Link
                key={model.id}
                href={`/models/${model.name}`}
                className="group rounded-xl border border-line bg-surface p-5 transition hover:border-line-strong hover:shadow-md"
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="flex items-center gap-3">
                    <span className="h-9 w-1.5 rounded-full" style={{ background: modelColor(model.name) }} />
                    <div>
                      <div className="font-mono text-[15px] font-semibold text-ink">{model.name}</div>
                      <div className="text-xs text-ink-3">{model.description || model.task_type}</div>
                    </div>
                  </div>
                  <ArrowRight size={16} className="mt-1 text-ink-3 transition group-hover:translate-x-0.5 group-hover:text-accent" />
                </div>

                <div className="mt-5 grid grid-cols-3 gap-3 text-sm">
                  <div>
                    <div className="text-[11px] text-ink-3">In production</div>
                    <div className="mt-0.5 font-medium text-ink">{production ? production.version : "none"}</div>
                  </div>
                  <div>
                    <div className="text-[11px] text-ink-3">Versions</div>
                    <div className="mt-0.5 font-medium text-ink">{versions.length}</div>
                  </div>
                  <div>
                    <div className="text-[11px] text-ink-3">Latest upload</div>
                    <div className="mt-0.5 font-medium text-ink">{latest ? ago(latest.created_at, now) : "—"}</div>
                  </div>
                </div>

                <div className="mt-4 flex flex-wrap gap-1.5">
                  {(["production", "staging", "registered", "archived"] as const)
                    .filter((s) => counts[s])
                    .map((s) => (
                      <span key={s} className="inline-flex items-center gap-1 text-xs text-ink-3">
                        <StageBadge stage={s} /> ×{counts[s]}
                      </span>
                    ))}
                </div>
              </Link>
            );
          })}
        </div>
      )}
    </>
  );
}

function CreateModel({ onDone }: { onDone: () => void }) {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(undefined);
    try {
      await go.createModel(name.trim(), description.trim());
      onDone();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card title="Register a new model" subtitle="Names become storage paths: letters, digits, dot, dash and underscore only." className="mb-6">
      <form onSubmit={submit} className="flex flex-col gap-3 sm:flex-row sm:items-end">
        <label className="flex-1 text-sm">
          <span className="mb-1 block text-xs font-medium text-ink-2">Name</span>
          <input
            required
            value={name}
            onChange={(e) => setName(e.target.value)}
            pattern="[A-Za-z0-9][A-Za-z0-9._\-]{0,127}"
            placeholder="loan_default_rf"
            className="w-full rounded-lg border border-line-strong bg-surface px-3 py-2 font-mono text-sm text-ink outline-none focus:border-accent"
          />
        </label>
        <label className="flex-[2] text-sm">
          <span className="mb-1 block text-xs font-medium text-ink-2">Description</span>
          <input
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="Random forest baseline"
            className="w-full rounded-lg border border-line-strong bg-surface px-3 py-2 text-sm text-ink outline-none focus:border-accent"
          />
        </label>
        <Button type="submit" loading={busy}>Create</Button>
      </form>
      {error && <p className="mt-2 text-sm text-critical">{error}</p>}
    </Card>
  );
}
