"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useMemo, useState } from "react";
import { ArrowLeft, Download, FileUp, PackageOpen, Upload } from "lucide-react";
import { BaselineChart, ConfusionMatrix, ShapChart } from "@/components/charts";
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
  cx,
  modelColor,
} from "@/components/ui";
import { go, type ModelVersion, type Stage } from "@/lib/api";
import { bytes, dateTime, pct } from "@/lib/format";
import { usePoll } from "@/lib/usePoll";

type Tab = "performance" | "explain" | "baseline" | "inputs" | "artifact";

export default function ModelDetailPage() {
  const { name } = useParams<{ name: string }>();
  const model = usePoll(() => go.model(name), 30000, [name]);
  const versions = usePoll(() => go.versions(name), 5000, [name]);
  const [selected, setSelected] = useState<string>();
  const [showUpload, setShowUpload] = useState(false);
  const [busy, setBusy] = useState<string>();
  const [actionError, setActionError] = useState<string>();

  const list = versions.data ?? [];
  const current =
    list.find((v) => v.version === selected) ?? list.find((v) => v.stage === "production") ?? list[0];
  const color = modelColor(name);

  const promote = async (v: ModelVersion, stage: Stage) => {
    setBusy(`${v.version}:${stage}`);
    setActionError(undefined);
    try {
      await go.promote(name, v.version, stage);
      versions.refresh();
    } catch (e) {
      setActionError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(undefined);
    }
  };

  return (
    <>
      <Link href="/models" className="mb-3 inline-flex items-center gap-1 text-xs font-medium text-ink-3 hover:text-ink">
        <ArrowLeft size={13} /> Model Registry
      </Link>
      <PageHeader
        title={name}
        subtitle={model.data?.description || "Versions, metrics, explainability and the training baseline used for drift detection."}
        actions={
          <>
            <LiveBadge updatedAt={versions.updatedAt} error={versions.error} intervalLabel="5s" />
            <Button onClick={() => setShowUpload((s) => !s)}>
              <Upload size={15} /> Upload version
            </Button>
          </>
        }
      />

      <Explainer title="What can I do here?">
        <p>
          <b>Promote</b> a version to <i>staging</i> to try it, or to <i>production</i> to make it the one served. The previous
          production version is archived in the same database transaction, so there is never a moment with two (or zero, if
          one existed) production versions.
        </p>
        <p>
          The tabs below describe the selected version: how it scored on held-out data, which inputs drive its predictions
          (SHAP), and the <b>training baseline</b>: the distribution of each input during training. The drift engine compares
          live traffic against these exact bins using PSI.
        </p>
      </Explainer>

      {showUpload && <UploadVersion name={name} onDone={() => { setShowUpload(false); versions.refresh(); }} />}
      <ErrorNote error={versions.error} what="the registry" />
      {actionError && <ErrorNote error={new Error(actionError)} what="promote" />}

      <Card title="Versions" subtitle="Newest first. Click a row to inspect it." pad={false} className="mb-6">
        {!versions.data && !versions.error ? (
          <div className="space-y-2 p-5">
            <Skeleton className="h-10" />
            <Skeleton className="h-10" />
          </div>
        ) : list.length === 0 ? (
          <EmptyState icon={<PackageOpen size={28} />} title="No versions uploaded yet">
            Use <b>Upload version</b> to add a trained model.onnx with its feature schema.
          </EmptyState>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[640px] text-sm">
              <thead>
                <tr className="border-b border-line text-left text-xs text-ink-3">
                  <th className="px-5 py-2.5 font-medium">Version</th>
                  <th className="px-3 py-2.5 font-medium">Stage</th>
                  <th className="px-3 py-2.5 text-right font-medium">ROC-AUC</th>
                  <th className="px-3 py-2.5 text-right font-medium">Recall</th>
                  <th className="px-3 py-2.5 text-right font-medium">F1</th>
                  <th className="hidden px-3 py-2.5 font-medium xl:table-cell">Uploaded</th>
                  <th className="px-5 py-2.5 text-right font-medium">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line">
                {list.map((v) => {
                  const isSel = current?.id === v.id;
                  return (
                    <tr
                      key={v.id}
                      onClick={() => setSelected(v.version)}
                      className={cx("cursor-pointer", isSel ? "bg-accent-soft/50" : "hover:bg-surface-2/60")}
                    >
                      <td className="px-5 py-3">
                        <span className="flex items-center gap-2 font-mono font-medium text-ink">
                          {isSel && <span className="h-4 w-1 rounded-full" style={{ background: color }} />}
                          {v.version}
                        </span>
                        <span className="text-xs text-ink-3">{v.framework || "—"} · {bytes(v.artifact_size)}</span>
                      </td>
                      <td className="px-3 py-3"><StageBadge stage={v.stage} /></td>
                      <td className="tabular px-3 py-3 text-right text-ink">{v.metrics?.roc_auc?.toFixed(3) ?? "—"}</td>
                      <td className="tabular px-3 py-3 text-right text-ink">{v.metrics?.recall?.toFixed(3) ?? "—"}</td>
                      <td className="tabular px-3 py-3 text-right text-ink">{v.metrics?.f1?.toFixed(3) ?? "—"}</td>
                      <td className="hidden px-3 py-3 text-xs text-ink-2 xl:table-cell">{dateTime(v.created_at)}</td>
                      <td className="px-5 py-3">
                        <div className="flex justify-end gap-1.5" onClick={(e) => e.stopPropagation()}>
                          {v.stage !== "production" && (
                            <Button variant="secondary" className="px-2 py-1 text-xs" loading={busy === `${v.version}:production`} onClick={() => promote(v, "production")}>
                              Promote to prod
                            </Button>
                          )}
                          {v.stage === "registered" && (
                            <Button variant="ghost" className="px-2 py-1 text-xs" loading={busy === `${v.version}:staging`} onClick={() => promote(v, "staging")}>
                              Stage
                            </Button>
                          )}
                          {v.stage !== "archived" && v.stage !== "production" && (
                            <Button variant="ghost" className="px-2 py-1 text-xs" loading={busy === `${v.version}:archived`} onClick={() => promote(v, "archived")}>
                              Archive
                            </Button>
                          )}
                          <a
                            href={go.artifactUrl(name, v.version)}
                            className="inline-flex items-center rounded-lg px-2 py-1 text-ink-3 hover:bg-surface-2 hover:text-ink"
                            title="Download model.onnx"
                          >
                            <Download size={14} />
                          </a>
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      {current && <VersionDetail v={current} color={color} />}
    </>
  );
}

function VersionDetail({ v, color }: { v: ModelVersion; color: string }) {
  const [tab, setTab] = useState<Tab>("performance");
  const tabs: [Tab, string][] = [
    ["performance", "Performance"],
    ["explain", "Explainability"],
    ["baseline", "Training baseline"],
    ["inputs", `Inputs (${v.feature_schema?.features?.length ?? 0})`],
    ["artifact", "Artifact"],
  ];
  return (
    <Card
      title={
        <span className="flex items-center gap-2">
          Version <span className="font-mono">{v.version}</span> <StageBadge stage={v.stage} />
        </span>
      }
      pad={false}
    >
      <div className="flex gap-1 overflow-x-auto border-b border-line px-3 [scrollbar-width:none]" role="tablist">
        {tabs.map(([id, label]) => (
          <button
            key={id}
            role="tab"
            aria-selected={tab === id}
            onClick={() => setTab(id)}
            className={cx(
              "-mb-px whitespace-nowrap border-b-2 px-3 py-2.5 text-sm transition",
              tab === id ? "border-accent font-medium text-ink" : "border-transparent text-ink-3 hover:text-ink",
            )}
          >
            {label}
          </button>
        ))}
      </div>
      <div className="p-5">
        {tab === "performance" && <Performance v={v} />}
        {tab === "explain" && <Explain v={v} color={color} />}
        {tab === "baseline" && <Baseline v={v} color={color} />}
        {tab === "inputs" && <Inputs v={v} />}
        {tab === "artifact" && <Artifact v={v} />}
      </div>
    </Card>
  );
}

function Performance({ v }: { v: ModelVersion }) {
  const m = v.metrics ?? {};
  if (m.accuracy == null && m.roc_auc == null) {
    return <EmptyState title="No metrics uploaded with this version">Attach a metrics JSON when uploading to see scores here.</EmptyState>;
  }
  const tiles: [string, number | undefined, string][] = [
    ["ROC-AUC", m.roc_auc, "How well it ranks defaulters above non-defaulters (0.5 = coin flip)"],
    ["Recall", m.recall, "Share of real defaults the model catches"],
    ["Precision", m.precision, "Of the loans it flags, how many actually default"],
    ["F1", m.f1, "Balance of precision and recall"],
    ["Accuracy", m.accuracy, "Share of all predictions that are right"],
  ];
  return (
    <div className="grid gap-6 lg:grid-cols-2">
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-2 xl:grid-cols-3">
        {tiles.map(([label, val, help]) => (
          <div key={label} className="rounded-lg border border-line p-3">
            <div className="text-xs text-ink-3">{label}</div>
            <div className="tabular mt-1 text-xl font-semibold text-ink">{label === "ROC-AUC" || label === "F1" ? val?.toFixed(3) ?? "—" : pct(val)}</div>
            <div className="mt-1 text-[11px] leading-snug text-ink-3">{help}</div>
          </div>
        ))}
      </div>
      {m.confusion_matrix && (
        <div>
          <div className="mb-2 text-xs font-medium text-ink-2">Confusion matrix on the test set</div>
          <ConfusionMatrix matrix={m.confusion_matrix} />
          <p className="mt-3 text-xs leading-relaxed text-ink-3">
            For lending, a <b>missed default</b> is the costly error, so these models are tuned toward recall: they raise more
            false alarms in exchange for catching most real defaults.
          </p>
        </div>
      )}
    </div>
  );
}

function Explain({ v, color }: { v: ModelVersion; color: string }) {
  const shap = v.metrics?.shap_importance;
  if (!shap?.length) return <EmptyState title="No SHAP importance stored for this version" />;
  return (
    <div className="grid gap-6 lg:grid-cols-3">
      <div className="lg:col-span-2">
        <ShapChart data={shap} color={color} top={12} />
      </div>
      <div className="space-y-3 text-sm leading-relaxed text-ink-2">
        <p>
          <b>SHAP</b> measures how much each input moves an individual prediction away from the average. Averaging the
          absolute values over 2,000 held-out applicants gives a global ranking of what the model relies on.
        </p>
        <p>
          The top features are the ones worth watching most closely for drift: a shift in <b>{shap[0].feature}</b> or{" "}
          <b>{shap[1]?.feature}</b> will move predictions far more than a shift in a low-ranked input.
        </p>
      </div>
    </div>
  );
}

function Baseline({ v, color }: { v: ModelVersion; color: string }) {
  const b = v.baseline_stats;
  const names = useMemo(() => (b ? b.feature_order ?? Object.keys(b.features) : []), [b]);
  const [feat, setFeat] = useState<string>();
  if (!b) return <EmptyState title="No baseline stats uploaded">Without a baseline the drift engine has nothing to compare live traffic against.</EmptyState>;
  const key = feat ?? names[0];
  const f = b.features[key];
  return (
    <div className="grid gap-6 lg:grid-cols-3">
      <div className="lg:col-span-2">
        <div className="mb-3 flex flex-wrap items-center gap-3">
          <label className="text-xs font-medium text-ink-2" htmlFor="feat">Feature</label>
          <select
            id="feat"
            value={key}
            onChange={(e) => setFeat(e.target.value)}
            className="rounded-lg border border-line-strong bg-surface px-2.5 py-1.5 text-sm text-ink"
          >
            {names.map((n) => (
              <option key={n} value={n}>{n}</option>
            ))}
          </select>
          <span className="text-xs text-ink-3">{f?.type} · {b.dataset.sample_count.toLocaleString()} training rows</span>
        </div>
        {f && <BaselineChart feature={f} color={color} />}
      </div>
      {f && (
        <div>
          <dl className="grid grid-cols-2 gap-2 text-sm">
            {(
              [
                ["Mean", f.mean],
                ["Std dev", f.std],
                ["Min", f.min],
                ["Max", f.max],
                ["Median", f.deciles?.p50],
                ["Missing", f.missing_rate],
              ] as const
            ).map(([k, val]) => (
              <div key={k} className="rounded-lg border border-line px-3 py-2">
                <dt className="text-[11px] text-ink-3">{k}</dt>
                <dd className="tabular font-medium text-ink">
                  {k === "Missing" ? pct(val) : val == null ? "—" : Math.abs(val) >= 1000 ? Math.round(val).toLocaleString() : +val.toFixed(3)}
                </dd>
              </div>
            ))}
          </dl>
          <p className="mt-3 text-xs leading-relaxed text-ink-3">
            Each bar is a decile bin of the training data. Live traffic is bucketed with the same edges; the
            <b> Population Stability Index</b> sums how far each bin&apos;s share has moved. PSI &lt; 0.1 is stable, 0.1–0.25 is a
            moderate shift, &gt; 0.25 is significant drift.
          </p>
        </div>
      )}
    </div>
  );
}

function Inputs({ v }: { v: ModelVersion }) {
  const features = v.feature_schema?.features ?? [];
  return (
    <div>
      <p className="mb-3 text-sm text-ink-2">
        The exact inputs, in order, that ml-service expects. Requests missing any of these, or carrying extra ones, are rejected
        with <code className="font-mono">INVALID_ARGUMENT</code> instead of silently predicting on wrong data.
      </p>
      <ol className="grid gap-1.5 sm:grid-cols-2 lg:grid-cols-3">
        {features.map((f, i) => (
          <li key={f} className="flex items-center gap-2 rounded-lg border border-line px-3 py-1.5 text-sm">
            <span className="tabular w-5 text-right text-xs text-ink-3">{i}</span>
            <span className="font-mono text-ink">{f}</span>
          </li>
        ))}
      </ol>
    </div>
  );
}

function Artifact({ v }: { v: ModelVersion }) {
  const rows: [string, React.ReactNode][] = [
    ["Storage key", <code key="k" className="font-mono">driftline-artifacts/{v.artifact_key}/model.onnx</code>],
    ["SHA-256", <code key="s" className="break-all font-mono text-xs">{v.checksum_sha256}</code>],
    ["Size", bytes(v.artifact_size)],
    ["Framework", v.framework || "—"],
    ["Uploaded", dateTime(v.created_at)],
    ["Last stage change", dateTime(v.promoted_at)],
    ["Version ID", <code key="i" className="font-mono text-xs">{v.id}</code>],
  ];
  return (
    <div className="grid gap-6 lg:grid-cols-3">
      <dl className="divide-y divide-line rounded-lg border border-line lg:col-span-2">
        {rows.map(([k, val]) => (
          <div key={k} className="grid grid-cols-3 gap-3 px-4 py-2.5 text-sm">
            <dt className="text-ink-3">{k}</dt>
            <dd className="col-span-2 text-ink">{val}</dd>
          </div>
        ))}
      </dl>
      <div className="space-y-3 text-sm text-ink-2">
        <p>
          The checksum was computed while the file streamed into MinIO, so the registry never buffers a whole model in memory.
          ml-service pulls this same object the first time a request names this version.
        </p>
        <a
          href={go.artifactUrl(v.model_name, v.version)}
          className="inline-flex items-center gap-1.5 rounded-lg border border-line-strong px-3 py-1.5 text-sm font-medium text-ink hover:bg-surface-2"
        >
          <Download size={14} /> Download model.onnx
        </a>
      </div>
    </div>
  );
}

function FilePick({ label, hint, accept, file, onFile, required }: {
  label: string;
  hint: string;
  accept: string;
  file?: File;
  onFile: (f?: File) => void;
  required?: boolean;
}) {
  return (
    <label
      className={cx(
        "flex cursor-pointer flex-col gap-1 rounded-lg border border-dashed px-4 py-3 transition hover:border-accent",
        file ? "border-accent bg-accent-soft/40" : "border-line-strong",
      )}
      onDragOver={(e) => e.preventDefault()}
      onDrop={(e) => {
        e.preventDefault();
        onFile(e.dataTransfer.files[0]);
      }}
    >
      <span className="flex items-center gap-2 text-sm font-medium text-ink">
        <FileUp size={15} className="text-ink-3" />
        {label} {required ? <span className="text-critical">*</span> : <span className="text-xs font-normal text-ink-3">optional</span>}
      </span>
      <span className="truncate text-xs text-ink-3">{file ? `${file.name} · ${bytes(file.size)}` : hint}</span>
      <input type="file" accept={accept} className="sr-only" onChange={(e) => onFile(e.target.files?.[0])} />
    </label>
  );
}

function UploadVersion({ name, onDone }: { name: string; onDone: () => void }) {
  const [version, setVersion] = useState("");
  const [model, setModel] = useState<File>();
  const [schema, setSchema] = useState<File>();
  const [metrics, setMetrics] = useState<File>();
  const [baseline, setBaseline] = useState<File>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!model || !schema) return;
    setBusy(true);
    setError(undefined);
    const form = new FormData();
    form.set("version", version.trim());
    form.set("model", model);
    form.set("feature_schema", schema);
    if (metrics) form.set("metrics", metrics);
    if (baseline) form.set("baseline_stats", baseline);
    try {
      await go.upload(name, form);
      onDone();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card title="Upload a new version" subtitle="Drop files or click to choose. The model is stored in MinIO; metadata in Postgres." className="mb-6">
      <form onSubmit={submit} className="space-y-4">
        <label className="block max-w-xs text-sm">
          <span className="mb-1 block text-xs font-medium text-ink-2">Version label</span>
          <input
            required
            value={version}
            onChange={(e) => setVersion(e.target.value)}
            pattern="[A-Za-z0-9][A-Za-z0-9._\-]{0,127}"
            placeholder="v2"
            className="w-full rounded-lg border border-line-strong bg-surface px-3 py-2 font-mono text-sm text-ink outline-none focus:border-accent"
          />
        </label>
        <div className="grid gap-3 sm:grid-cols-2">
          <FilePick label="model.onnx" hint="Exported ONNX model" accept=".onnx" file={model} onFile={setModel} required />
          <FilePick label="feature_schema.json" hint="Ordered list of input features" accept=".json" file={schema} onFile={setSchema} required />
          <FilePick label="metrics.json" hint="Test-set metrics (+ shap_importance)" accept=".json" file={metrics} onFile={setMetrics} />
          <FilePick label="baseline_stats.json" hint="Training distribution, for drift" accept=".json" file={baseline} onFile={setBaseline} />
        </div>
        {error && <p className="text-sm text-critical">{error}</p>}
        <Button type="submit" loading={busy} disabled={!model || !schema || !version}>
          <Upload size={15} /> Register version
        </Button>
      </form>
    </Card>
  );
}
