"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { CheckCircle2, Code2, RotateCcw, ShieldAlert, ShieldCheck, XCircle } from "lucide-react";
import {
  Card,
  EmptyState,
  ErrorNote,
  Explainer,
  PageHeader,
  Skeleton,
  StageBadge,
  cx,
  modelColor,
} from "@/components/ui";
import { ml, type ModelVersion, type PredictResult } from "@/lib/api";
import { CATEGORICAL, FLAGS, LOAN_TERMS, NUMERIC_FIELDS, TEST_APPLICANTS, encode, medianApplicant, rangeFor, type RawApplicant } from "@/lib/features";
import { useRegistry } from "@/lib/hooks";

type Outcome = { key: string; model: string; version: string; result?: PredictResult; error?: string; at: number };
type HistoryRow = { at: number; source: string; outcomes: Outcome[]; actual?: boolean };

export default function PredictPage() {
  const registry = useRegistry(30000);
  const entries = useMemo(() => registry.data ?? [], [registry.data]);

  // Which model/versions to score against: default = every production version.
  const [picked, setPicked] = useState<Record<string, string>>({});
  const targets: ModelVersion[] = useMemo(() => {
    const out: ModelVersion[] = [];
    for (const e of entries) {
      const choice = picked[e.model.name] ?? e.production?.version ?? "";
      if (choice === "off") continue;
      const v = e.versions.find((x) => x.version === choice);
      if (v) out.push(v);
    }
    return out;
  }, [entries, picked]);

  const baselineOwner = targets.find((t) => t.baseline_stats);
  const baseline = baselineOwner?.baseline_stats;
  // Until the user edits something, the applicant is the training-set median.
  // Memoised on the version id so a registry refresh doesn't re-trigger scoring.
  const hasData = !!registry.data;
  const median = useMemo(
    () => (hasData ? medianApplicant(baseline) : null),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [baselineOwner?.id, hasData],
  );
  const [edited, setRaw] = useState<RawApplicant | null>(null);
  const raw = edited ?? median;
  const [source, setSource] = useState<{ label: string; actual?: boolean }>({ label: "Median applicant" });

  const [outcomes, setOutcomes] = useState<Outcome[]>([]);
  const [pending, setPending] = useState(false);
  const [history, setHistory] = useState<HistoryRow[]>([]);
  const [showPayload, setShowPayload] = useState(false);
  const seq = useRef(0);
  // Registry polling returns fresh objects; key the effect on identity, not reference.
  const targetKey = targets.map((t) => t.id).join(",");
  const targetsRef = useRef(targets);
  const sourceRef = useRef(source);
  useEffect(() => {
    targetsRef.current = targets;
    sourceRef.current = source;
  });

  // Re-score (debounced) whenever the applicant or the targets change.
  useEffect(() => {
    const targets = targetsRef.current;
    if (!raw || targets.length === 0) return;
    const id = ++seq.current;
    setPending(true);
    const timer = setTimeout(async () => {
      const results = await Promise.all(
        targets.map(async (v): Promise<Outcome> => {
          const key = `${v.model_name}/${v.version}`;
          try {
            const result = await ml.predict(v.model_name, v.version, encode(raw, v.feature_schema.features), `lab-${Date.now()}`);
            return { key, model: v.model_name, version: v.version, result, at: Date.now() };
          } catch (e) {
            return { key, model: v.model_name, version: v.version, error: e instanceof Error ? e.message : String(e), at: Date.now() };
          }
        }),
      );
      if (id !== seq.current) return; // a newer request superseded this one
      setOutcomes(results);
      setPending(false);
      const src = sourceRef.current;
      setHistory((h) => [{ at: Date.now(), source: src.label, outcomes: results, actual: src.actual }, ...h].slice(0, 12));
    }, 350);
    return () => clearTimeout(timer);
  }, [raw, targetKey]);

  const set = <K extends keyof RawApplicant>(k: K, val: RawApplicant[K]) => {
    if (raw) setRaw({ ...raw, [k]: val });
    setSource((s) => (s.actual === undefined ? s : { label: `${s.label} (edited)` }));
  };

  const payloadTarget = targets[0];

  return (
    <>
      <PageHeader
        title="Prediction Lab"
        subtitle="Score a loan application against the live models. Every change re-runs inference through ml-service in real time."
      />

      <Explainer title="What happens when I change a value?">
        <p>
          The form holds the 16 fields a loan officer sees. The browser one-hot encodes them into the model&apos;s 24 inputs
          (exactly as training did), and sends a request to <b>ml-service</b>, which runs the ONNX model with ONNX Runtime and
          returns per-class probabilities plus the inference latency.
        </p>
        <p>
          Load one of the <b>real test-set applicants</b> to see whether each model gets a case it has never seen right. Each
          request is also counted in the <b>Live Monitoring</b> page&apos;s traffic and latency charts.
        </p>
      </Explainer>
      <ErrorNote error={registry.error} what="the registry" />

      {!registry.data && !registry.error ? (
        <Skeleton className="h-96" />
      ) : entries.length === 0 ? (
        <Card>
          <EmptyState title="No models registered">Register a model in the Model Registry first.</EmptyState>
        </Card>
      ) : (
        <div className="grid gap-6 xl:grid-cols-5">
          <div className="space-y-6 xl:col-span-3">
            <Card title="Models to score" subtitle="Defaults to each model's production version">
              <div className="grid gap-3 sm:grid-cols-2">
                {entries.map((e) => {
                  const value = picked[e.model.name] ?? e.production?.version ?? "off";
                  return (
                    <label key={e.model.id} className="flex items-center gap-3 rounded-lg border border-line px-3 py-2">
                      <span className="h-6 w-1 rounded-full" style={{ background: modelColor(e.model.name) }} />
                      <span className="flex-1 truncate font-mono text-sm text-ink">{e.model.name}</span>
                      <select
                        value={value}
                        onChange={(ev) => setPicked((p) => ({ ...p, [e.model.name]: ev.target.value }))}
                        className="rounded-md border border-line-strong bg-surface px-2 py-1 text-xs text-ink"
                      >
                        <option value="off">off</option>
                        {e.versions.filter((v) => v.stage !== "archived").map((v) => (
                          <option key={v.id} value={v.version}>
                            {v.version} · {v.stage}
                          </option>
                        ))}
                      </select>
                    </label>
                  );
                })}
              </div>
            </Card>

            <Card
              title="Applicant"
              subtitle={source.label}
              actions={
                <button
                  onClick={() => { setRaw(null); setSource({ label: "Median applicant" }); }}
                  className="flex items-center gap-1 text-xs font-medium text-ink-3 hover:text-ink"
                >
                  <RotateCcw size={12} /> Reset to median
                </button>
              }
            >
              <div className="mb-5">
                <div className="mb-2 text-xs font-medium text-ink-2">Real applicants from the held-out test set</div>
                <div className="flex flex-wrap gap-2">
                  {TEST_APPLICANTS.map((a) => (
                    <button
                      key={a.id}
                      onClick={() => { setRaw(a.raw); setSource({ label: `Test applicant ${a.id}`, actual: a.defaulted }); }}
                      className={cx(
                        "rounded-lg border px-2.5 py-1.5 text-left text-xs transition hover:border-accent",
                        source.label === `Test applicant ${a.id}` ? "border-accent bg-accent-soft/50" : "border-line",
                      )}
                    >
                      <span className="font-mono text-ink">{a.id}</span>
                      <span className="ml-1.5 text-ink-3">actually {a.defaulted ? "defaulted" : "repaid"}</span>
                    </button>
                  ))}
                </div>
              </div>

              {raw && (
                <div className="space-y-6">
                  <div className="grid gap-x-6 gap-y-4 sm:grid-cols-2">
                    {NUMERIC_FIELDS.map((f) => {
                      const { min, max } = rangeFor(f.key, baseline);
                      const val = raw[f.key] as number;
                      return (
                        <div key={f.key}>
                          <div className="flex items-baseline justify-between text-sm">
                            <label htmlFor={f.key} className="font-medium text-ink">{f.label}</label>
                            <span className="tabular text-ink-2">{f.format(val)}</span>
                          </div>
                          <input
                            id={f.key}
                            type="range"
                            min={min}
                            max={max}
                            step={f.step}
                            value={val}
                            onChange={(e) => set(f.key, Number(e.target.value) as never)}
                            className="mt-1.5 w-full accent-[var(--accent)]"
                          />
                          <div className="flex justify-between text-[10px] text-ink-3">
                            <span>{f.format(min)}</span>
                            <span>{f.help}</span>
                            <span>{f.format(max)}</span>
                          </div>
                        </div>
                      );
                    })}
                  </div>

                  <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
                    <Select label="Loan term" value={String(raw.LoanTerm)} options={LOAN_TERMS.map(String)} suffix=" months" onChange={(v) => set("LoanTerm", Number(v))} />
                    {(Object.keys(CATEGORICAL) as (keyof typeof CATEGORICAL)[]).map((k) => (
                      <Select key={k} label={CATEGORICAL[k].label} value={raw[k]} options={CATEGORICAL[k].options} onChange={(v) => set(k, v)} />
                    ))}
                  </div>

                  <div className="flex flex-wrap gap-2">
                    {FLAGS.map((f) => (
                      <button
                        key={f.key}
                        onClick={() => set(f.key, !raw[f.key])}
                        aria-pressed={raw[f.key]}
                        className={cx(
                          "rounded-full border px-3 py-1 text-xs font-medium transition",
                          raw[f.key] ? "border-accent bg-accent text-white" : "border-line-strong text-ink-2 hover:border-accent",
                        )}
                      >
                        {f.label}: {raw[f.key] ? "Yes" : "No"}
                      </button>
                    ))}
                  </div>
                </div>
              )}
            </Card>
          </div>

          <div className="space-y-6 xl:col-span-2">
            <div className="xl:sticky xl:top-6 space-y-6">
              <Card
                title="Prediction"
                subtitle={pending ? "Scoring…" : "Default probability per model, decision threshold 50%"}
                actions={pending ? <span className="h-2 w-2 rounded-full bg-accent live-dot" /> : undefined}
              >
                {targets.length === 0 ? (
                  <EmptyState title="Pick at least one model version" />
                ) : outcomes.length === 0 ? (
                  <Skeleton className="h-32" />
                ) : (
                  <div className="space-y-4">
                    {outcomes.map((o) => (
                      <ResultRow key={o.key} o={o} actual={source.actual} stage={targets.find((t) => `${t.model_name}/${t.version}` === o.key)?.stage} />
                    ))}
                  </div>
                )}

                {payloadTarget && raw && (
                  <div className="mt-5 border-t border-line pt-3">
                    <button onClick={() => setShowPayload((s) => !s)} className="flex items-center gap-1.5 text-xs font-medium text-ink-3 hover:text-ink">
                      <Code2 size={13} /> {showPayload ? "Hide" : "Show"} the encoded request ({payloadTarget.feature_schema.features.length} features)
                    </button>
                    {showPayload && (
                      <pre className="mt-2 max-h-72 overflow-auto rounded-lg bg-surface-2 p-3 font-mono text-[11px] leading-relaxed text-ink-2">
                        {JSON.stringify(
                          { model_name: payloadTarget.model_name, model_version: payloadTarget.version, features: encode(raw, payloadTarget.feature_schema.features) },
                          null,
                          2,
                        )}
                      </pre>
                    )}
                  </div>
                )}
              </Card>

              <Card title="This session" subtitle="Your last 12 scoring runs" pad={false}>
                {history.length === 0 ? (
                  <EmptyState title="Nothing scored yet" />
                ) : (
                  <ul className="max-h-80 divide-y divide-line overflow-auto">
                    {history.map((h) => (
                      <li key={h.at} className="px-5 py-2.5 text-xs">
                        <div className="flex justify-between text-ink-3">
                          <span className="truncate">{h.source}</span>
                          <span>{new Date(h.at).toLocaleTimeString()}</span>
                        </div>
                        <div className="mt-1 flex flex-wrap gap-x-4 gap-y-1">
                          {h.outcomes.map((o) => (
                            <span key={o.key} className="flex items-center gap-1.5">
                              <span className="h-2 w-2 rounded-full" style={{ background: modelColor(o.model) }} />
                              <span className="font-mono text-ink-2">{o.model.replace("loan_default_", "")}</span>
                              <span className="tabular font-medium text-ink">
                                {o.result ? `${(o.result.probabilities[1] * 100).toFixed(1)}%` : "error"}
                              </span>
                            </span>
                          ))}
                        </div>
                      </li>
                    ))}
                  </ul>
                )}
              </Card>
            </div>
          </div>
        </div>
      )}
    </>
  );
}

function Select({ label, value, options, onChange, suffix = "" }: { label: string; value: string; options: string[]; onChange: (v: string) => void; suffix?: string }) {
  return (
    <label className="text-sm">
      <span className="mb-1 block font-medium text-ink">{label}</span>
      <select value={value} onChange={(e) => onChange(e.target.value)} className="w-full rounded-lg border border-line-strong bg-surface px-2.5 py-1.5 text-sm text-ink">
        {options.map((o) => (
          <option key={o} value={o}>{o}{suffix}</option>
        ))}
      </select>
    </label>
  );
}

function ResultRow({ o, actual, stage }: { o: Outcome; actual?: boolean; stage?: ModelVersion["stage"] }) {
  const color = modelColor(o.model);
  if (!o.result) {
    return (
      <div className="rounded-lg border border-critical/30 p-3 text-sm">
        <div className="font-mono text-ink">{o.key}</div>
        <div className="mt-1 text-critical">{o.error}</div>
      </div>
    );
  }
  const p = o.result.probabilities[1];
  const defaults = o.result.prediction === 1;
  const correct = actual === undefined ? undefined : actual === defaults;
  return (
    <div className="rounded-lg border border-line p-4">
      <div className="flex items-center justify-between gap-2">
        <span className="flex items-center gap-2 font-mono text-sm text-ink">
          <span className="h-3 w-3 rounded-sm" style={{ background: color }} />
          {o.model} <span className="text-ink-3">{o.version}</span>
        </span>
        {stage && <StageBadge stage={stage} />}
      </div>
      <div className="mt-3 flex items-end justify-between gap-3">
        <div>
          <div className="tabular text-3xl font-semibold tracking-tight text-ink">{(p * 100).toFixed(1)}%</div>
          <div className="text-xs text-ink-3">probability of default</div>
        </div>
        <span
          className={cx(
            "inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-medium",
            defaults ? "bg-critical/12 text-critical" : "bg-good/12 text-good-ink",
          )}
        >
          {defaults ? <ShieldAlert size={13} /> : <ShieldCheck size={13} />}
          {defaults ? "High risk: likely default" : "Low risk: likely repays"}
        </span>
      </div>
      {/* probability bar with the 50% threshold marked */}
      <div className="relative mt-3 h-2 rounded-full bg-surface-2">
        <div className="h-2 rounded-full transition-all duration-300" style={{ width: `${p * 100}%`, background: color }} />
        <div className="absolute -top-1 left-1/2 h-4 w-px bg-ink-3" title="decision threshold" />
      </div>
      <div className="mt-2 flex flex-wrap items-center justify-between gap-2 text-[11px] text-ink-3">
        <span>
          inference {Number(o.result.latency_ms) < 1 ? "<1" : o.result.latency_ms} ms · req <span className="font-mono">{o.result.request_id}</span>
        </span>
        {correct !== undefined && (
          <span className={cx("inline-flex items-center gap-1 font-medium", correct ? "text-good-ink" : "text-critical")}>
            {correct ? <CheckCircle2 size={12} /> : <XCircle size={12} />}
            {correct ? "matches the real outcome" : "differs from the real outcome"}
          </span>
        )}
      </div>
    </div>
  );
}
