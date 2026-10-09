import type { ReactNode } from "react";
import { ArrowDown, ArrowRight, CheckCircle2, CircleDashed } from "lucide-react";
import { Card, PageHeader } from "@/components/ui";

export const metadata = { title: "How it works · Driftline" };

type Status = "live" | "building";

const COMPONENTS: { name: string; where: string; status: Status; what: string; why: string }[] = [
  {
    name: "Training pipeline",
    where: "ml/projects/loan_default",
    status: "live",
    what: "Cleans 255k loan records, trains Logistic Regression + XGBoost with class weighting, evaluates on a held-out 20%, computes SHAP importance and the training baseline, and exports both to ONNX.",
    why: "ONNX gives one portable file per model, so serving doesn't need scikit-learn or XGBoost installed. An automatic check proves the ONNX copy predicts identically (max difference 6e-7).",
  },
  {
    name: "Model Registry",
    where: "go/internal/registry",
    status: "live",
    what: "REST API to create models, upload versions, promote them through staging → production, and download artifacts.",
    why: "One source of truth for what is deployed. Uploads stream to MinIO while hashing; if the DB write fails, the upload is deleted, so storage and database never disagree.",
  },
  {
    name: "Artifact storage",
    where: "go/internal/storage · MinIO",
    status: "live",
    what: "S3-compatible bucket holding model.onnx + feature_schema.json under {model}/{version}/.",
    why: "Model files are large binaries that don't belong in Postgres. Both the Go registry and ml-service use the same key layout.",
  },
  {
    name: "ml-service",
    where: "ml-service/",
    status: "live",
    what: "Python gRPC server running ONNX Runtime. Validates features against the schema, caches one inference session per version, and exports Prometheus metrics.",
    why: "gRPC + protobuf gives a typed, fast contract between Go and Python on the hottest path. If a version isn't on disk, the service pulls it from MinIO on first use.",
  },
  {
    name: "Alert service",
    where: "go/internal/alert",
    status: "live",
    what: "Sends drift alerts to Slack and Discord in parallel, with retries, a per-version cooldown, and a record of every attempt.",
    why: "A Go interface (AlertNotifier) means adding email or PagerDuty is a new type, not a rewrite.",
  },
  {
    name: "Prediction proxy",
    where: "go/internal/prediction",
    status: "building",
    what: "Public /predict endpoint that routes traffic between versions (canary splits) over gRPC and logs every request.",
    why: "Lets a new version take 10% of traffic before it takes 100%.",
  },
  {
    name: "Drift scheduler",
    where: "go/internal/drift",
    status: "building",
    what: "Periodically compares logged live inputs against each version's baseline bins using PSI / KS-test.",
    why: "Catches the world changing under the model before accuracy visibly drops.",
  },
  {
    name: "Rollback manager",
    where: "go/internal/rollback",
    status: "building",
    what: "Re-promotes the previous good version when drift or errors cross a threshold.",
    why: "Closes the loop: detect → alert → recover without a human awake at 3am.",
  },
];

const LIFECYCLE = [
  ["Train", "Python script trains both models and writes model.onnx, feature_schema.json, metrics and baseline_stats.json."],
  ["Register", "Upload the files to the registry. The Go service stores the model in MinIO and the metadata in Postgres."],
  ["Promote", "Move a version to production. The previous production version is archived atomically."],
  ["Serve", "ml-service loads the ONNX file (from disk or MinIO) and answers predictions over gRPC in milliseconds."],
  ["Monitor", "Traffic and latency are exported as metrics; live inputs are compared against the training baseline."],
  ["Alert & recover", "Drift triggers Slack/Discord alerts; rollback can re-promote the last good version."],
];

const API: [string, string, string][] = [
  ["GET", "/healthz", "Postgres + MinIO health"],
  ["POST", "/api/v1/models", "Create a model"],
  ["GET", "/api/v1/models", "List models"],
  ["POST", "/api/v1/models/{name}/versions", "Upload a version (multipart: version, model, feature_schema, metrics?, baseline_stats?)"],
  ["GET", "/api/v1/models/{name}/versions", "List versions with metrics + baseline"],
  ["POST", "/api/v1/models/{name}/versions/{v}/promote", '{"stage": "staging" | "production" | "archived"}'],
  ["GET", "/api/v1/models/{name}/versions/{v}/artifact", "Download model.onnx"],
  ["GET", "/api/v1/alerts", "Recent alert attempts"],
  ["POST", "/api/v1/alerts/test", "Send a sample alert to every channel"],
  ["gRPC", "PredictionService.Predict", "ml-service :50051: model_name, model_version, features map → probabilities"],
];

const GLOSSARY: [string, ReactNode][] = [
  ["Data drift", "Live inputs no longer look like the training data (e.g. incomes rise with inflation). The model was never taught this new world, so its predictions quietly degrade."],
  ["PSI", "Population Stability Index. Bucket a feature with the training deciles, compare the share of rows per bucket now vs. then. < 0.1 stable, 0.1–0.25 moderate, > 0.25 significant."],
  ["SHAP", "A per-prediction attribution: how much each input pushed the prediction up or down. Averaged over many rows it ranks which features the model relies on."],
  ["ROC-AUC", "The probability the model scores a random defaulter above a random non-defaulter. 0.5 is a coin flip, 1.0 is perfect. It doesn't depend on the decision threshold."],
  ["Recall / precision", "Recall: share of real defaults caught. Precision: share of flagged loans that really default. Raising one usually lowers the other."],
  ["ONNX", "Open Neural Network Exchange: a framework-neutral file format for trained models, executed by ONNX Runtime."],
  ["gRPC", "A binary RPC protocol over HTTP/2 using protobuf schemas. Faster than JSON and type-checked on both ends."],
  ["Canary", "Sending a small share of traffic to a new version first, and promoting it only if it behaves."],
];

function StatusTag({ s }: { s: Status }) {
  return s === "live" ? (
    <span className="inline-flex items-center gap-1 text-xs font-medium text-good-ink">
      <CheckCircle2 size={13} /> Running
    </span>
  ) : (
    <span className="inline-flex items-center gap-1 text-xs font-medium text-ink-3">
      <CircleDashed size={13} /> In progress
    </span>
  );
}

function Box({ title, sub, dashed }: { title: string; sub: string; dashed?: boolean }) {
  return (
    <div className={dashed ? "rounded-lg border border-dashed border-line-strong px-3 py-2 text-center opacity-75" : "rounded-lg border border-line-strong bg-surface-2 px-3 py-2 text-center"}>
      <div className="text-xs font-semibold text-ink">{title}</div>
      <div className="text-[11px] text-ink-3">{sub}</div>
    </div>
  );
}

export default function GuidePage() {
  return (
    <>
      <PageHeader
        title="How Driftline works"
        subtitle="Every component, what it does, why it is built that way, and how the pieces fit together."
      />

      <Card title="Architecture" subtitle="Solid boxes are running today; dashed ones are in progress" className="mb-6">
        <div className="mx-auto flex max-w-3xl flex-col items-center gap-2">
          <Box title="Next.js dashboard" sub="this app · REST/JSON" />
          <ArrowDown size={16} className="text-ink-3" />
          <div className="w-full rounded-xl border border-line p-3">
            <div className="mb-2 text-center text-[11px] font-medium uppercase tracking-wide text-ink-3">Go control plane :8080</div>
            <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
              <Box title="Model Registry" sub="versions & stages" />
              <Box title="Alert Service" sub="Slack · Discord" />
              <Box title="Prediction Proxy" sub="canary routing" dashed />
              <Box title="Drift Scheduler" sub="PSI · KS-test" dashed />
              <Box title="Rollback Manager" sub="auto-recover" dashed />
              <Box title="Deployment Mgr" sub="containers" dashed />
            </div>
          </div>
          <div className="grid w-full grid-cols-3 gap-2">
            <div className="flex flex-col items-center gap-2">
              <ArrowDown size={16} className="text-ink-3" />
              <Box title="PostgreSQL" sub="metadata · logs" />
            </div>
            <div className="flex flex-col items-center gap-2">
              <ArrowDown size={16} className="text-ink-3" />
              <Box title="MinIO" sub="model files" />
            </div>
            <div className="flex flex-col items-center gap-2">
              <span className="flex items-center gap-1 text-[11px] text-ink-3">
                <ArrowDown size={16} /> gRPC
              </span>
              <Box title="ml-service" sub="ONNX Runtime" />
            </div>
          </div>
          <p className="mt-2 max-w-xl text-center text-xs text-ink-3">
            ml-service pulls model files straight from MinIO, so registering a version in the dashboard is enough for it to be servable.
          </p>
        </div>
      </Card>

      <Card title="The model lifecycle" className="mb-6">
        <ol className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {LIFECYCLE.map(([step, text], i) => (
            <li key={step} className="relative rounded-lg border border-line p-4">
              <div className="flex items-center gap-2">
                <span className="flex h-6 w-6 items-center justify-center rounded-full bg-accent text-xs font-semibold text-white">{i + 1}</span>
                <span className="text-sm font-semibold text-ink">{step}</span>
                {i < LIFECYCLE.length - 1 && <ArrowRight size={14} className="ml-auto text-ink-3" />}
              </div>
              <p className="mt-2 text-sm leading-relaxed text-ink-2">{text}</p>
            </li>
          ))}
        </ol>
      </Card>

      <h2 className="mb-3 text-lg font-semibold text-ink">Components</h2>
      <div className="mb-6 grid gap-4 md:grid-cols-2">
        {COMPONENTS.map((c) => (
          <div key={c.name} className="rounded-xl border border-line bg-surface p-5">
            <div className="flex items-start justify-between gap-3">
              <div>
                <h3 className="text-sm font-semibold text-ink">{c.name}</h3>
                <code className="text-[11px] text-ink-3">{c.where}</code>
              </div>
              <StatusTag s={c.status} />
            </div>
            <p className="mt-3 text-sm leading-relaxed text-ink-2">{c.what}</p>
            <p className="mt-2 text-sm leading-relaxed text-ink-3">
              <b className="font-medium text-ink-2">Why: </b>
              {c.why}
            </p>
          </div>
        ))}
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        <Card title="API reference" pad={false}>
          <ul className="divide-y divide-line">
            {API.map(([method, path, desc]) => (
              <li key={method + path} className="px-5 py-2.5">
                <div className="flex items-center gap-2">
                  <span className="w-11 shrink-0 rounded bg-surface-2 py-0.5 text-center font-mono text-[10px] font-semibold text-ink-2">{method}</span>
                  <code className="truncate font-mono text-xs text-ink">{path}</code>
                </div>
                <p className="mt-0.5 pl-13 text-xs text-ink-3">{desc}</p>
              </li>
            ))}
          </ul>
        </Card>

        <Card title="Glossary" pad={false}>
          <dl className="divide-y divide-line">
            {GLOSSARY.map(([term, def]) => (
              <div key={term} className="px-5 py-3">
                <dt className="text-sm font-semibold text-ink">{term}</dt>
                <dd className="mt-0.5 text-sm leading-relaxed text-ink-2">{def}</dd>
              </div>
            ))}
          </dl>
        </Card>
      </div>
    </>
  );
}
