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
    what: "REST API to create models, upload versions, move them through stages, and download artifacts.",
    why: "One source of truth for what exists. Uploads stream to MinIO while hashing; if the DB write fails the upload is deleted, so storage and database never disagree.",
  },
  {
    name: "Deployment Manager",
    where: "go/internal/deployment",
    status: "live",
    what: "Deploys a version as the primary (blue/green) or as a canary with N% of traffic, changes the split, promotes, stops and rolls back. Warms the model in ml-service before it gets traffic.",
    why: "Traffic shares are updated in one transaction so they always sum to 100%, and a per-model lock stops two releases racing each other.",
  },
  {
    name: "Prediction Proxy",
    where: "go/internal/prediction",
    status: "live",
    what: "The public POST /predict/{model} endpoint. Picks primary or canary by weight, calls ml-service over gRPC, and logs every request.",
    why: "A shared atomic counter makes a 90/10 split exact on every 100 requests. Logs go through a bounded buffer and are written in batches with COPY, so the database never slows a prediction down.",
  },
  {
    name: "ml-service",
    where: "ml-service/",
    status: "live",
    what: "Python gRPC server running ONNX Runtime. Validates features against the schema, caches one inference session per version, loads/unloads on demand, exports Prometheus metrics.",
    why: "gRPC + protobuf gives a typed, fast contract between Go and Python on the hottest path. Versions not on disk are pulled from MinIO on first use.",
  },
  {
    name: "Drift Scheduler",
    where: "go/internal/drift",
    status: "live",
    what: "Every minute, compares the recent window of logged inputs with each serving version's training deciles: PSI per feature plus a binned Kolmogorov-Smirnov test.",
    why: "Catches the world changing under the model before accuracy visibly drops. Versions are checked concurrently through a bounded worker pool.",
  },
  {
    name: "Rollback Manager",
    where: "go/internal/rollback",
    status: "live",
    what: "Rolls a canary back automatically when its error rate or predicted-default rate diverges from the primary's, or when it drifts while the primary doesn't. Manual rollback restores the previous primary.",
    why: "Drift on the primary itself is not rolled back (an older model sees the same new data); it triggers a retraining recommendation instead.",
  },
  {
    name: "Alerts & observability",
    where: "go/internal/alert · infra/",
    status: "live",
    what: "Slack and Discord notifications for drift, rollbacks and retraining advice, every attempt recorded. Prometheus scrapes both services; Grafana ships with a Driftline dashboard.",
    why: "A Go interface (AlertNotifier) makes channels pluggable; metrics are labelled by route pattern and model so cardinality stays bounded.",
  },
];

const LIFECYCLE = [
  ["Train", "Python script trains both models and writes model.onnx, feature_schema.json, metrics and baseline_stats.json."],
  ["Register", "Upload the files to the registry. The Go service stores the model in MinIO and the metadata in Postgres."],
  ["Release", "Deploy the new version as a canary with a slice of traffic, watch it, then promote it to primary."],
  ["Serve", "ml-service loads the ONNX file (from disk or MinIO) and answers predictions over gRPC in milliseconds."],
  ["Monitor", "Traffic and latency are exported as metrics; live inputs are compared against the training baseline."],
  ["Alert & recover", "Drift triggers alerts and a retraining recommendation; unhealthy canaries are rolled back automatically. scripts/retrain.py closes the loop."],
];

const API: [string, string, string][] = [
  ["GET", "/healthz", "Postgres, MinIO and ml-service health"],
  ["GET", "/metrics", "Prometheus metrics"],
  ["POST", "/api/v1/models", "Create a model"],
  ["GET", "/api/v1/models", "List models"],
  ["POST", "/api/v1/models/{name}/versions", "Upload a version (multipart: version, model, feature_schema, metrics?, baseline_stats?)"],
  ["GET", "/api/v1/models/{name}/versions", "List versions with metrics + baseline"],
  ["POST", "/api/v1/models/{name}/versions/{v}/promote", '{"stage": "staging" | "production" | "archived"}'],
  ["GET", "/api/v1/models/{name}/versions/{v}/artifact", "Download model.onnx"],
  ["POST", "/api/v1/predict/{model}", '{"features": {...}} → routed prediction (primary or canary), logged'],
  ["GET", "/api/v1/predict/{model}/routes", "Current routing table"],
  ["POST", "/api/v1/deployments", '{"model","version","traffic_percent"}: primary at 100, canary below'],
  ["PATCH", "/api/v1/deployments/{id}/traffic", "Change a canary's share"],
  ["POST", "/api/v1/deployments/{id}/promote", "Canary → primary"],
  ["POST", "/api/v1/deployments/rollback", '{"model"}: undo the last release'],
  ["GET", "/api/v1/drift/reports", "Drift reports (PSI/KS per feature)"],
  ["POST", "/api/v1/drift/run", "Run a drift check now"],
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

      <Card title="Architecture" subtitle="Every component below is running" className="mb-6">
        <div className="mx-auto flex max-w-3xl flex-col items-center gap-2">
          <Box title="Next.js dashboard" sub="this app · REST/JSON" />
          <ArrowDown size={16} className="text-ink-3" />
          <div className="w-full rounded-xl border border-line p-3">
            <div className="mb-2 text-center text-[11px] font-medium uppercase tracking-wide text-ink-3">Go control plane :8080</div>
            <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
              <Box title="Model Registry" sub="versions & stages" />
              <Box title="Alert Service" sub="Slack · Discord" />
              <Box title="Prediction Proxy" sub="canary routing" />
              <Box title="Drift Scheduler" sub="PSI · KS-test" />
              <Box title="Rollback Manager" sub="auto-recover" />
              <Box title="Deployment Manager" sub="primary + canary" />
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
          <div className="mt-1 grid w-full grid-cols-2 gap-2 sm:w-2/3">
            <Box title="Prometheus" sub="scrapes both services" />
            <Box title="Grafana :3001" sub="Driftline dashboard" />
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
