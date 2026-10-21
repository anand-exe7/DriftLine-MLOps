"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import {
  Briefcase,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  Clock,
  Code2,
  CreditCard,
  DollarSign,
  Gauge,
  Percent,
  RotateCcw,
  ShieldAlert,
  ShieldCheck,
  Sliders,
  Sparkles,
  TrendingDown,
  TrendingUp,
  User,
  XCircle,
  Zap,
} from "lucide-react";
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
import { go, ml, type PredictResult } from "@/lib/api";
import {
  CATEGORICAL,
  FLAGS,
  LOAN_TERMS,
  encode,
  medianApplicant,
  type RawApplicant,
} from "@/lib/features";
import { useRegistry } from "@/lib/hooks";

type Outcome = {
  key: string;
  model: string;
  version: string;
  role?: string;
  result?: PredictResult;
  error?: string;
  at: number;
};

// 3 Simple, Executive Presets for Rapid Demonstrations
const PRESET_PERSONAS = [
  {
    id: "prime",
    title: "Prime Borrower",
    desc: "$138k Income · 603 FICO · $227k Loan",
    badge: "Approved (Low Risk)",
    badgeCls: "bg-emerald-500/15 text-emerald-800 dark:text-emerald-300 border-emerald-500/30",
    data: {
      Age: 49,
      Income: 138499,
      LoanAmount: 85000,
      CreditScore: 780,
      MonthsEmployed: 96,
      NumCreditLines: 3,
      InterestRate: 4.5,
      LoanTerm: 36,
      DTIRatio: 0.22,
      Education: "Master's",
      EmploymentType: "Full-time",
      MaritalStatus: "Married",
      HasMortgage: true,
      HasDependents: false,
      LoanPurpose: "Home",
      HasCoSigner: true,
    } as RawApplicant,
    actual: false,
  },
  {
    id: "median",
    title: "Average Applicant",
    desc: "$82k Income · 574 FICO · $127k Loan",
    badge: "Baseline (Moderate)",
    badgeCls: "bg-amber-500/15 text-amber-800 dark:text-amber-300 border-amber-500/30",
    data: null, // will use training-set median
    actual: undefined,
  },
  {
    id: "subprime",
    title: "Subprime Borrower",
    desc: "$24k Income · 420 FICO · $246k Loan",
    badge: "High Risk (Default)",
    badgeCls: "bg-rose-500/15 text-rose-800 dark:text-rose-300 border-rose-500/30",
    data: {
      Age: 22,
      Income: 24000,
      LoanAmount: 240000,
      CreditScore: 420,
      MonthsEmployed: 4,
      NumCreditLines: 4,
      InterestRate: 21.5,
      LoanTerm: 60,
      DTIRatio: 0.78,
      Education: "High School",
      EmploymentType: "Unemployed",
      MaritalStatus: "Single",
      HasMortgage: false,
      HasDependents: true,
      LoanPurpose: "Other",
      HasCoSigner: false,
    } as RawApplicant,
    actual: true,
  },
];

export default function PredictPage() {
  const registry = useRegistry(30000);
  const entries = useMemo(() => registry.data ?? [], [registry.data]);

  const [mode, setMode] = useState<"proxy" | "direct">("proxy");
  const [selectedModel, setSelectedModel] = useState<string>("loan_default_lr");
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [activePreset, setActivePreset] = useState<string>("median");

  const baselineOwner = entries.find((e) => e.model.name === selectedModel)?.production;
  const baseline = baselineOwner?.baseline_stats;
  const median = useMemo(() => medianApplicant(baseline), [baseline]);

  const [raw, setRaw] = useState<RawApplicant>(median);
  const [outcome, setOutcome] = useState<Outcome | null>(null);
  const [pending, setPending] = useState(false);
  const [showJson, setShowJson] = useState(false);
  const seq = useRef(0);

  // Sync to median initially
  useEffect(() => {
    if (activePreset === "median") {
      const t = setTimeout(() => setRaw(median), 0);
      return () => clearTimeout(t);
    }
  }, [median, activePreset]);

  // Real-time live inference on every input change (debounced 200ms)
  useEffect(() => {
    const entry = entries.find((e) => e.model.name === selectedModel);
    if (!entry || !entry.production) return;
    const v = entry.production;

    const id = ++seq.current;
    const timer = setTimeout(async () => {
      setPending(true);
      const features = encode(raw, v.feature_schema.features);
      if (mode === "proxy") {
        try {
          const r = await go.predict(v.model_name, features);
          const result: PredictResult = {
            prediction: r.prediction,
            probabilities: r.probabilities,
            model_version_used: r.version,
            latency_ms: r.inference_ms,
            request_id: r.request_id,
            status_code: 0,
            error_message: "",
          };
          if (id === seq.current) {
            setOutcome({ key: v.model_name, model: v.model_name, version: r.version, role: r.role, result, at: Date.now() });
            setPending(false);
          }
        } catch (e) {
          if (id === seq.current) {
            setOutcome({ key: v.model_name, model: v.model_name, version: "?", error: String(e), at: Date.now() });
            setPending(false);
          }
        }
      } else {
        try {
          const result = await ml.predict(v.model_name, v.version, features, `lab-${Date.now()}`);
          if (id === seq.current) {
            setOutcome({ key: v.model_name, model: v.model_name, version: v.version, result, at: Date.now() });
            setPending(false);
          }
        } catch (e) {
          if (id === seq.current) {
            setOutcome({ key: v.model_name, model: v.model_name, version: v.version, error: String(e), at: Date.now() });
            setPending(false);
          }
        }
      }
    }, 200);

    return () => clearTimeout(timer);
  }, [raw, selectedModel, mode, entries]);

  const updateField = <K extends keyof RawApplicant>(k: K, val: RawApplicant[K]) => {
    setActivePreset("custom");
    setRaw((prev) => ({ ...prev, [k]: val }));
  };

  const currentEntry = entries.find((e) => e.model.name === selectedModel);
  const activeVersion = currentEntry?.production;

  return (
    <>
      <PageHeader
        title="Prediction Lab"
        subtitle="Live inference cockpit powered by ONNX Runtime and Go proxy. Test applicant profiles and observe real-time default risk predictions."
        actions={
          <div className="flex items-center gap-2">
            <span className="inline-flex items-center gap-1.5 rounded-full border border-line bg-surface px-3 py-1 text-xs font-semibold text-ink-2 shadow-xs">
              <span className="h-2 w-2 rounded-full bg-emerald-500 live-dot" /> Live gRPC to ml-service
            </span>
          </div>
        }
      />

      <div className="space-y-6">
        {/* Quick Persona Selector - ONE CLICK DEMO */}
        <div className="rounded-2xl border border-line bg-surface p-5 shadow-xs">
          <div className="mb-3 flex items-center justify-between">
            <span className="flex items-center gap-2 text-sm font-bold text-ink">
              <Sparkles size={16} className="text-accent" /> 1-Click Applicant Presets
            </span>
            <span className="text-xs text-ink-3">Click any profile to simulate an applicant instant live score</span>
          </div>

          <div className="grid gap-3 sm:grid-cols-3">
            {PRESET_PERSONAS.map((p) => {
              const isSelected = activePreset === p.id;
              return (
                <button
                  key={p.id}
                  onClick={() => {
                    setActivePreset(p.id);
                    setRaw(p.data ?? median);
                  }}
                  className={cx(
                    "flex flex-col items-start rounded-xl border p-4 text-left transition-all hover:border-accent hover:shadow-xs",
                    isSelected
                      ? "border-accent bg-accent-soft/80 shadow-xs ring-2 ring-accent/30"
                      : "border-line bg-surface hover:bg-surface-2/60",
                  )}
                >
                  <div className="flex w-full items-center justify-between gap-1">
                    <span className="font-bold text-ink text-base">{p.title}</span>
                    <span className={cx("rounded-full border px-2.5 py-0.5 text-xs font-bold", p.badgeCls)}>
                      {p.badge}
                    </span>
                  </div>
                  <p className="mt-1 text-xs font-medium text-ink-2">{p.desc}</p>
                </button>
              );
            })}
          </div>
        </div>

        {/* Main 2-Column Split: Simple 4 Sliders on Left, Live Decision on Right */}
        <div className="grid gap-6 xl:grid-cols-12">
          {/* Left Column: Core 4 Controls + Model Switcher */}
          <div className="space-y-6 xl:col-span-7">
            {/* Top Toolbar: Active Model & Mode */}
            <div className="flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-line bg-surface p-4 shadow-xs">
              <div className="flex items-center gap-2">
                <span className="text-xs font-bold uppercase tracking-wider text-ink-3">Target Model:</span>
                <div className="flex gap-1.5">
                  {entries.map((e) => (
                    <button
                      key={e.model.id}
                      onClick={() => setSelectedModel(e.model.name)}
                      className={cx(
                        "rounded-lg px-3 py-1.5 font-mono text-xs font-bold transition active:scale-95",
                        selectedModel === e.model.name
                          ? "bg-accent text-white shadow-xs"
                          : "border border-line bg-surface-2 text-ink-2 hover:bg-surface",
                      )}
                    >
                      {e.model.name}
                    </button>
                  ))}
                </div>
              </div>

              <div className="flex items-center gap-2">
                <span className="text-xs font-bold uppercase tracking-wider text-ink-3">Routing:</span>
                <button
                  onClick={() => setMode((m) => (m === "proxy" ? "direct" : "proxy"))}
                  className="rounded-lg border border-line-strong bg-surface-2 px-3 py-1 text-xs font-semibold text-ink hover:bg-surface"
                >
                  {mode === "proxy" ? "Go Proxy (Live Traffic Split)" : "Direct ONNX Inference"}
                </button>
              </div>
            </div>

            {/* Core 4 Sliders Card - Clean, Big, Intuitive */}
            <div className="rounded-2xl border border-line bg-surface p-6 shadow-xs space-y-6">
              <div className="flex items-center justify-between border-b border-line pb-3">
                <div>
                  <h3 className="text-base font-bold text-ink flex items-center gap-2">
                    <Sliders size={18} className="text-accent" /> Key Application Parameters
                  </h3>
                  <p className="text-xs text-ink-3">Top features influencing loan default risk (SHAP ranking)</p>
                </div>
                {activePreset === "custom" && (
                  <button
                    onClick={() => {
                      setActivePreset("median");
                      setRaw(median);
                    }}
                    className="inline-flex items-center gap-1.5 text-xs font-semibold text-ink-3 hover:text-ink"
                  >
                    <RotateCcw size={12} /> Reset to Median
                  </button>
                )}
              </div>

              <div className="space-y-6">
                {/* 1. Loan Amount */}
                <div className="space-y-2">
                  <div className="flex items-center justify-between">
                    <label className="text-sm font-bold text-ink flex items-center gap-2">
                      <CreditCard size={16} className="text-blue-600" /> Loan Amount Requested
                    </label>
                    <span className="tabular font-bold font-mono text-base text-accent bg-accent-soft px-3 py-1 rounded-lg">
                      ${Math.round(raw.LoanAmount).toLocaleString()}
                    </span>
                  </div>
                  <input
                    type="range"
                    min={5000}
                    max={250000}
                    step={1000}
                    value={raw.LoanAmount}
                    onChange={(e) => updateField("LoanAmount", Number(e.target.value))}
                    className="h-2.5 w-full cursor-pointer appearance-none rounded-lg bg-surface-2 accent-[var(--accent)]"
                  />
                  <div className="flex justify-between text-xs text-ink-3 font-medium">
                    <span>$5,000</span>
                    <span>Median: $127,000</span>
                    <span>$250,000</span>
                  </div>
                </div>

                {/* 2. Annual Income */}
                <div className="space-y-2">
                  <div className="flex items-center justify-between">
                    <label className="text-sm font-bold text-ink flex items-center gap-2">
                      <DollarSign size={16} className="text-emerald-600" /> Annual Gross Income
                    </label>
                    <span className="tabular font-bold font-mono text-base text-accent bg-accent-soft px-3 py-1 rounded-lg">
                      ${Math.round(raw.Income).toLocaleString()}
                    </span>
                  </div>
                  <input
                    type="range"
                    min={15000}
                    max={150000}
                    step={1000}
                    value={raw.Income}
                    onChange={(e) => updateField("Income", Number(e.target.value))}
                    className="h-2.5 w-full cursor-pointer appearance-none rounded-lg bg-surface-2 accent-[var(--accent)]"
                  />
                  <div className="flex justify-between text-xs text-ink-3 font-medium">
                    <span>$15,000</span>
                    <span>Median: $82,000</span>
                    <span>$150,000</span>
                  </div>
                </div>

                {/* 3. Credit Score */}
                <div className="space-y-2">
                  <div className="flex items-center justify-between">
                    <label className="text-sm font-bold text-ink flex items-center gap-2">
                      <Gauge size={16} className="text-amber-600" /> Credit Score (FICO)
                    </label>
                    <div className="flex items-center gap-2">
                      <span className="text-xs font-semibold px-2 py-0.5 rounded-md bg-surface-2 text-ink-2">
                        {raw.CreditScore >= 740 ? "Excellent" : raw.CreditScore >= 670 ? "Good" : raw.CreditScore >= 580 ? "Fair" : "Poor"}
                      </span>
                      <span className="tabular font-bold font-mono text-base text-accent bg-accent-soft px-3 py-1 rounded-lg">
                        {raw.CreditScore}
                      </span>
                    </div>
                  </div>
                  <input
                    type="range"
                    min={300}
                    max={850}
                    step={1}
                    value={raw.CreditScore}
                    onChange={(e) => updateField("CreditScore", Number(e.target.value))}
                    className="h-2.5 w-full cursor-pointer appearance-none rounded-lg bg-surface-2 accent-[var(--accent)]"
                  />
                  <div className="flex justify-between text-xs text-ink-3 font-medium">
                    <span>300 (Poor)</span>
                    <span>574 (Median)</span>
                    <span>850 (Exceptional)</span>
                  </div>
                </div>

                {/* 4. Interest Rate */}
                <div className="space-y-2">
                  <div className="flex items-center justify-between">
                    <label className="text-sm font-bold text-ink flex items-center gap-2">
                      <Percent size={16} className="text-purple-600" /> Offered Interest Rate
                    </label>
                    <span className="tabular font-bold font-mono text-base text-accent bg-accent-soft px-3 py-1 rounded-lg">
                      {raw.InterestRate.toFixed(2)}%
                    </span>
                  </div>
                  <input
                    type="range"
                    min={2}
                    max={25}
                    step={0.1}
                    value={raw.InterestRate}
                    onChange={(e) => updateField("InterestRate", Number(e.target.value))}
                    className="h-2.5 w-full cursor-pointer appearance-none rounded-lg bg-surface-2 accent-[var(--accent)]"
                  />
                  <div className="flex justify-between text-xs text-ink-3 font-medium">
                    <span>2.0%</span>
                    <span>13.5% (Median)</span>
                    <span>25.0%</span>
                  </div>
                </div>
              </div>

              {/* Optional Collapsible for Remaining 12 Secondary Parameters */}
              <div className="border-t border-line pt-4">
                <button
                  type="button"
                  onClick={() => setShowAdvanced((s) => !s)}
                  className="flex w-full items-center justify-between rounded-xl bg-surface-2 px-4 py-2.5 text-xs font-semibold text-ink-2 hover:bg-surface-2/80 transition"
                >
                  <span className="flex items-center gap-2">
                    <User size={15} />
                    {showAdvanced ? "Hide" : "Show"} Additional Applicant Details (Term, Employment, Collateral, Education)
                  </span>
                  <ChevronDown size={16} className={cx("transition-transform", showAdvanced && "rotate-180")} />
                </button>

                {showAdvanced && (
                  <div className="mt-5 space-y-5 rounded-xl border border-line bg-surface p-5">
                    <div className="grid gap-4 sm:grid-cols-2">
                      {/* Debt-to-income */}
                      <div>
                        <div className="flex justify-between text-xs font-semibold text-ink mb-1">
                          <span>Debt-to-Income (DTI)</span>
                          <span className="font-mono">{raw.DTIRatio.toFixed(2)}</span>
                        </div>
                        <input
                          type="range"
                          min={0.1}
                          max={0.9}
                          step={0.01}
                          value={raw.DTIRatio}
                          onChange={(e) => updateField("DTIRatio", Number(e.target.value))}
                          className="h-2 w-full cursor-pointer appearance-none rounded-lg bg-surface-2 accent-[var(--accent)]"
                        />
                      </div>

                      {/* Months Employed */}
                      <div>
                        <div className="flex justify-between text-xs font-semibold text-ink mb-1">
                          <span>Employment Tenure</span>
                          <span className="font-mono">{raw.MonthsEmployed} mos</span>
                        </div>
                        <input
                          type="range"
                          min={0}
                          max={119}
                          step={1}
                          value={raw.MonthsEmployed}
                          onChange={(e) => updateField("MonthsEmployed", Number(e.target.value))}
                          className="h-2 w-full cursor-pointer appearance-none rounded-lg bg-surface-2 accent-[var(--accent)]"
                        />
                      </div>

                      {/* Loan Term */}
                      <div>
                        <label className="block text-xs font-semibold text-ink mb-1.5">Loan Term</label>
                        <div className="flex flex-wrap gap-1.5">
                          {LOAN_TERMS.map((t) => (
                            <button
                              key={t}
                              type="button"
                              onClick={() => updateField("LoanTerm", t)}
                              className={cx(
                                "rounded-md border px-2.5 py-1 text-xs font-semibold",
                                raw.LoanTerm === t ? "border-accent bg-accent text-white" : "border-line bg-surface text-ink-2",
                              )}
                            >
                              {t}m
                            </button>
                          ))}
                        </div>
                      </div>

                      {/* Loan Purpose */}
                      <div>
                        <label className="block text-xs font-semibold text-ink mb-1.5">Purpose</label>
                        <select
                          value={raw.LoanPurpose}
                          onChange={(e) => updateField("LoanPurpose", e.target.value)}
                          className="w-full rounded-lg border border-line-strong bg-surface px-2.5 py-1 text-xs text-ink font-medium"
                        >
                          {CATEGORICAL.LoanPurpose.options.map((o) => (
                            <option key={o} value={o}>{o}</option>
                          ))}
                        </select>
                      </div>

                      {/* Employment Type */}
                      <div>
                        <label className="block text-xs font-semibold text-ink mb-1.5">Employment Type</label>
                        <select
                          value={raw.EmploymentType}
                          onChange={(e) => updateField("EmploymentType", e.target.value)}
                          className="w-full rounded-lg border border-line-strong bg-surface px-2.5 py-1 text-xs text-ink font-medium"
                        >
                          {CATEGORICAL.EmploymentType.options.map((o) => (
                            <option key={o} value={o}>{o}</option>
                          ))}
                        </select>
                      </div>

                      {/* Education */}
                      <div>
                        <label className="block text-xs font-semibold text-ink mb-1.5">Education</label>
                        <select
                          value={raw.Education}
                          onChange={(e) => updateField("Education", e.target.value)}
                          className="w-full rounded-lg border border-line-strong bg-surface px-2.5 py-1 text-xs text-ink font-medium"
                        >
                          {CATEGORICAL.Education.options.map((o) => (
                            <option key={o} value={o}>{o}</option>
                          ))}
                        </select>
                      </div>
                    </div>

                    {/* Flags */}
                    <div>
                      <label className="block text-xs font-semibold text-ink mb-1.5">Collateral & Dependents</label>
                      <div className="flex flex-wrap gap-2">
                        {FLAGS.map((f) => (
                          <button
                            key={f.key}
                            type="button"
                            onClick={() => updateField(f.key, !raw[f.key])}
                            className={cx(
                              "rounded-lg border px-3 py-1 text-xs font-semibold transition",
                              raw[f.key] ? "border-accent bg-accent text-white" : "border-line bg-surface text-ink-2",
                            )}
                          >
                            {f.label}: {raw[f.key] ? "Yes" : "No"}
                          </button>
                        ))}
                      </div>
                    </div>
                  </div>
                )}
              </div>
            </div>
          </div>

          {/* Right Column: Live Decision Cockpit (Sticky) */}
          <div className="space-y-6 xl:col-span-5">
            <div className="xl:sticky xl:top-6 space-y-6">
              {/* Massive Decision Hero Card */}
              <div className="rounded-2xl border border-line bg-surface p-6 shadow-sm">
                <div className="flex items-center justify-between border-b border-line pb-4">
                  <div>
                    <h3 className="text-lg font-bold tracking-tight text-ink flex items-center gap-2">
                      <Gauge size={20} className="text-accent" /> Live Scoring Outcome
                    </h3>
                    <p className="text-xs text-ink-3">Calculated live via Python ONNX runtime</p>
                  </div>
                  {pending ? (
                    <span className="inline-flex items-center gap-1.5 text-xs font-bold text-accent">
                      <span className="h-2 w-2 rounded-full bg-accent live-dot" /> Scoring...
                    </span>
                  ) : (
                    <span className="inline-flex items-center gap-1 text-xs font-semibold text-emerald-700 dark:text-emerald-400">
                      <CheckCircle2 size={13} /> Active
                    </span>
                  )}
                </div>

                {outcome?.result ? (
                  <div className="mt-5 space-y-5">
                    {/* Hero Decision Banner */}
                    <div
                      className={cx(
                        "rounded-2xl border p-5 text-center transition-all",
                        outcome.result.prediction === 1
                          ? "border-rose-500/30 bg-rose-500/10 text-rose-800 dark:text-rose-200"
                          : "border-emerald-500/30 bg-emerald-500/10 text-emerald-800 dark:text-emerald-200",
                      )}
                    >
                      <div className="flex items-center justify-center gap-2">
                        {outcome.result.prediction === 1 ? (
                          <ShieldAlert size={28} className="text-rose-600 dark:text-rose-400" />
                        ) : (
                          <ShieldCheck size={28} className="text-emerald-600 dark:text-emerald-400" />
                        )}
                        <span className="text-xl font-extrabold uppercase tracking-tight">
                          {outcome.result.prediction === 1 ? "High Default Risk (Rejected)" : "Low Default Risk (Approved)"}
                        </span>
                      </div>

                      {/* Big Probability Metric */}
                      <div className="mt-4">
                        <div className="text-5xl font-black font-mono tracking-tight text-ink">
                          {(outcome.result.probabilities[1] * 100).toFixed(1)}%
                        </div>
                        <div className="mt-1 text-xs font-semibold uppercase tracking-wider text-ink-3">
                          Calculated Default Probability
                        </div>
                      </div>

                      {/* Probability Progress Bar with 50% Cutoff */}
                      <div className="relative mt-5">
                        <div className="h-3.5 w-full overflow-hidden rounded-full bg-surface ring-1 ring-line">
                          <div
                            className={cx(
                              "h-full rounded-full transition-all duration-300",
                              outcome.result.prediction === 1 ? "bg-rose-500" : "bg-emerald-500",
                            )}
                            style={{ width: `${Math.min(100, Math.max(4, outcome.result.probabilities[1] * 100))}%` }}
                          />
                        </div>
                        <div className="absolute -top-1 left-1/2 h-5 w-0.5 bg-ink-2 opacity-60" title="50% Threshold" />
                        <div className="mt-1.5 flex justify-between text-[11px] font-semibold text-ink-3">
                          <span>0% (Safe)</span>
                          <span className="text-ink font-bold">50% Decision Cutoff</span>
                          <span>100% (Default)</span>
                        </div>
                      </div>
                    </div>

                    {/* Micro-Telemetry & System Lineage */}
                    <div className="rounded-xl border border-line bg-surface-2/60 p-4 space-y-2 text-xs">
                      <div className="flex items-center justify-between">
                        <span className="text-ink-3 font-medium">Model Serving Version:</span>
                        <span className="font-mono font-bold text-ink">{selectedModel} ({outcome.version})</span>
                      </div>
                      <div className="flex items-center justify-between">
                        <span className="text-ink-3 font-medium">Inference Execution:</span>
                        <span className="font-mono font-bold text-emerald-700 dark:text-emerald-400 flex items-center gap-1">
                          <Zap size={13} /> {Number(outcome.result.latency_ms) < 1 ? "<1" : outcome.result.latency_ms} ms (gRPC)
                        </span>
                      </div>
                      <div className="flex items-center justify-between">
                        <span className="text-ink-3 font-medium">Transaction Log ID:</span>
                        <span className="font-mono text-ink-3 truncate max-w-[180px]">{outcome.result.request_id}</span>
                      </div>
                    </div>
                  </div>
                ) : outcome?.error ? (
                  <div className="mt-5 rounded-xl border border-rose-500/30 bg-rose-500/10 p-4 text-xs text-rose-700">
                    {outcome.error}
                  </div>
                ) : (
                  <Skeleton className="mt-5 h-56" />
                )}

                {/* Inspect ONNX Vector Button */}
                {activeVersion && (
                  <div className="mt-5 border-t border-line pt-4">
                    <button
                      type="button"
                      onClick={() => setShowJson((s) => !s)}
                      className="inline-flex items-center gap-2 text-xs font-semibold text-ink-2 hover:text-accent transition"
                    >
                      <Code2 size={14} /> {showJson ? "Hide" : "Inspect"} 24-feature ONNX input vector
                    </button>
                    {showJson && (
                      <pre className="mt-3 max-h-56 overflow-auto rounded-xl bg-surface-2 p-3 font-mono text-[11px] leading-relaxed text-ink-2 shadow-inner">
                        {JSON.stringify(encode(raw, activeVersion.feature_schema.features), null, 2)}
                      </pre>
                    )}
                  </div>
                )}
              </div>
            </div>
          </div>
        </div>
      </div>
    </>
  );
}
