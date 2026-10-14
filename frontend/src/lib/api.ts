// Typed client for the two backends, reached through the rewrites in next.config.ts.

export type Stage = "registered" | "staging" | "production" | "archived";

export interface Model {
  id: string;
  name: string;
  description: string;
  task_type: string;
  created_at: string;
  updated_at: string;
}

export interface EvalMetrics {
  accuracy?: number;
  precision?: number;
  recall?: number;
  f1?: number;
  roc_auc?: number;
  confusion_matrix?: number[][];
  shap_importance?: { feature: string; mean_abs_shap: number }[];
}

export interface FeatureSchema {
  model_name?: string;
  framework?: string;
  features: string[];
}

export interface BaselineFeature {
  type: "binary" | "continuous";
  mean: number;
  std: number;
  min: number;
  max: number;
  missing_rate: number;
  deciles: Record<string, number>;
  bins: { edges: number[]; proportions: number[] };
}

export interface BaselineStats {
  dataset: { feature_count: number; sample_count: number; source?: string };
  feature_order?: string[];
  features: Record<string, BaselineFeature>;
}

export interface ModelVersion {
  id: string;
  model_id: string;
  model_name: string;
  version: string;
  framework: string;
  artifact_key: string;
  artifact_size: number;
  checksum_sha256: string;
  feature_schema: FeatureSchema;
  metrics: EvalMetrics;
  baseline_stats?: BaselineStats | null;
  stage: Stage;
  created_at: string;
  promoted_at?: string | null;
}

export interface Alert {
  id: string;
  model_version_id?: string;
  drift_report_id?: string;
  severity: "info" | "warning" | "critical";
  channel: string;
  message: string;
  status: "sent" | "failed";
  error?: string;
  created_at: string;
}

export interface PredictResult {
  prediction: number;
  probabilities: number[];
  model_version_used: string;
  latency_ms: string | number;
  request_id: string;
  status_code: number;
  error_message: string;
}

export interface Deployment {
  id: string;
  model_version_id: string;
  model_name: string;
  version: string;
  role: "primary" | "canary";
  status: "pending" | "running" | "stopped" | "failed" | "rolled_back";
  status_reason?: string;
  traffic_percent: number;
  endpoint: string;
  started_at?: string | null;
  stopped_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface RouteEntry {
  deployment_id?: string;
  version_id: string;
  model_name: string;
  version: string;
  weight: number;
  role: "primary" | "canary" | "production";
}

export interface ProxyResult {
  request_id: string;
  model: string;
  version: string;
  role: RouteEntry["role"];
  deployment_id?: string;
  prediction: number;
  probabilities: number[];
  inference_ms: number;
  total_ms: number;
}

export interface VersionStats {
  model_version_id: string;
  model_name: string;
  version: string;
  requests: number;
  errors: number;
  default_rate: number;
  avg_latency_ms: number;
}

export interface FeatureDrift {
  psi: number;
  ks_stat?: number;
  ks_p?: number;
}

export interface DriftReport {
  id: string;
  model_version_id: string;
  model_name: string;
  version: string;
  window_start: string;
  window_end: string;
  sample_size: number;
  overall_psi: number;
  drift_detected: boolean;
  feature_scores: Record<string, FeatureDrift>;
  created_at: string;
}

export interface DriftConfig {
  interval: string;
  window: string;
  min_samples: number;
  feature_psi_trigger: number;
  overall_psi_trigger: number;
}

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
  }
}

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, { cache: "no-store", ...init });
  const text = await res.text();
  let body: unknown = null;
  try {
    body = text ? JSON.parse(text) : null;
  } catch {
    body = text;
  }
  if (!res.ok) {
    const msg =
      (body && typeof body === "object" && ("error" in body ? (body as { error: string }).error : "detail" in body ? String((body as { detail: unknown }).detail) : null)) ||
      `${res.status} ${res.statusText}`;
    throw new ApiError(res.status, String(msg));
  }
  return body as T;
}

const json = (data: unknown): RequestInit => ({
  method: "POST",
  headers: { "content-type": "application/json" },
  body: JSON.stringify(data),
});

export const go = {
  // 503 still carries {"postgres": "...", "minio": "...", "ml_service": "..."} detail, so read it either way.
  healthz: async () => {
    const res = await fetch("/go/healthz", { cache: "no-store" });
    if (res.status !== 200 && res.status !== 503) throw new ApiError(res.status, "control plane unreachable");
    return (await res.json()) as Record<string, string>;
  },
  models: () => request<Model[]>("/go/api/v1/models"),
  model: (name: string) => request<Model>(`/go/api/v1/models/${encodeURIComponent(name)}`),
  createModel: (name: string, description: string) =>
    request<Model>("/go/api/v1/models", json({ name, description })),
  deleteModel: (name: string) => request<null>(`/go/api/v1/models/${encodeURIComponent(name)}`, { method: "DELETE" }),
  versions: (name: string) => request<ModelVersion[]>(`/go/api/v1/models/${encodeURIComponent(name)}/versions`),
  promote: (name: string, version: string, stage: Stage) =>
    request<ModelVersion>(
      `/go/api/v1/models/${encodeURIComponent(name)}/versions/${encodeURIComponent(version)}/promote`,
      json({ stage }),
    ),
  upload: (name: string, form: FormData) =>
    request<ModelVersion>(`/go/api/v1/models/${encodeURIComponent(name)}/versions`, { method: "POST", body: form }),
  artifactUrl: (name: string, version: string) =>
    `/go/api/v1/models/${encodeURIComponent(name)}/versions/${encodeURIComponent(version)}/artifact`,
  alerts: (limit = 100) => request<Alert[]>(`/go/api/v1/alerts?limit=${limit}`),
  alertChannels: () => request<{ channels: string[] }>("/go/api/v1/alerts/channels"),
  testAlert: () => request<{ sent_to: string[] }>("/go/api/v1/alerts/test", { method: "POST" }),

  deployments: (model = "") => request<Deployment[]>(`/go/api/v1/deployments?model=${encodeURIComponent(model)}`),
  deploy: (model: string, version: string, traffic_percent: number) =>
    request<Deployment>("/go/api/v1/deployments", json({ model, version, traffic_percent })),
  setTraffic: (id: string, traffic_percent: number) =>
    request<Deployment>(`/go/api/v1/deployments/${id}/traffic`, { ...json({ traffic_percent }), method: "PATCH" }),
  promoteDeployment: (id: string) => request<Deployment>(`/go/api/v1/deployments/${id}/promote`, { method: "POST" }),
  stopDeployment: (id: string) => request<Deployment>(`/go/api/v1/deployments/${id}/stop`, { method: "POST" }),
  rollback: (model: string, reason: string) => request<Deployment>("/go/api/v1/deployments/rollback", json({ model, reason })),

  routes: (model: string) => request<RouteEntry[]>(`/go/api/v1/predict/${encodeURIComponent(model)}/routes`),
  stats: (model: string, window = "15m") =>
    request<{ window: string; versions: VersionStats[] }>(`/go/api/v1/predict/${encodeURIComponent(model)}/stats?window=${window}`),
  predict: (model: string, features: Record<string, number>, routing_key = "") =>
    request<ProxyResult>(`/go/api/v1/predict/${encodeURIComponent(model)}`, json({ features, ...(routing_key ? { routing_key } : {}) })),

  driftReports: (model = "", limit = 100) =>
    request<DriftReport[]>(`/go/api/v1/drift/reports?model=${encodeURIComponent(model)}&limit=${limit}`),
  driftConfig: () => request<DriftConfig>("/go/api/v1/drift/config"),
  runDrift: () => request<DriftReport[]>("/go/api/v1/drift/run", { method: "POST" }),
};

export const ml = {
  health: () => request<{ status: string; loaded_models: string[] }>("/ml/health"),
  models: () => request<{ available: string[]; loaded: string[] }>("/ml/models"),
  metricsText: async () => {
    const res = await fetch("/ml/metrics", { cache: "no-store" });
    if (!res.ok) throw new ApiError(res.status, "metrics unavailable");
    return res.text();
  },
  predict: (model_name: string, model_version: string, features: Record<string, number>, request_id = "") =>
    request<PredictResult>("/ml/predict", json({ model_name, model_version, features, request_id })),
};
