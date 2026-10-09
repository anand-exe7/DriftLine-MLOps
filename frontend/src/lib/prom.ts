// Minimal Prometheus text-format parser for ml-service's /metrics, plus helpers
// to turn the raw counters/histograms into numbers a dashboard can show.

export interface Sample {
  name: string;
  labels: Record<string, string>;
  value: number;
}

const LINE = /^([a-zA-Z_:][a-zA-Z0-9_:]*)(\{([^}]*)\})?\s+(\S+)/;
const LABEL = /([a-zA-Z_][a-zA-Z0-9_]*)="((?:[^"\\]|\\.)*)"/g;

export function parseProm(text: string): Sample[] {
  const out: Sample[] = [];
  for (const line of text.split("\n")) {
    if (!line || line.startsWith("#")) continue;
    const m = LINE.exec(line);
    if (!m) continue;
    const labels: Record<string, string> = {};
    if (m[3]) for (const l of m[3].matchAll(LABEL)) labels[l[1]] = l[2];
    out.push({ name: m[1], labels, value: Number(m[4]) });
  }
  return out;
}

export interface ModelTraffic {
  key: string; // "model/version"
  model: string;
  version: string;
  ok: number;
  errors: number;
  latencyCount: number;
  latencySum: number; // seconds
  buckets: { le: number; count: number }[]; // cumulative
}

/** Groups predictions_total + inference_seconds by model/version. */
export function trafficByModel(samples: Sample[]): Map<string, ModelTraffic> {
  const map = new Map<string, ModelTraffic>();
  const get = (model: string, version: string) => {
    const key = `${model}/${version}`;
    let t = map.get(key);
    if (!t) {
      t = { key, model, version, ok: 0, errors: 0, latencyCount: 0, latencySum: 0, buckets: [] };
      map.set(key, t);
    }
    return t;
  };

  for (const s of samples) {
    const { model, version } = s.labels;
    if (!model || !version) continue;
    if (s.name === "mlservice_predictions_total") {
      const t = get(model, version);
      if (s.labels.status === "ok") t.ok += s.value;
      else t.errors += s.value;
    } else if (s.name === "mlservice_inference_seconds_bucket") {
      get(model, version).buckets.push({ le: s.labels.le === "+Inf" ? Infinity : Number(s.labels.le), count: s.value });
    } else if (s.name === "mlservice_inference_seconds_count") {
      get(model, version).latencyCount = s.value;
    } else if (s.name === "mlservice_inference_seconds_sum") {
      get(model, version).latencySum = s.value;
    }
  }
  for (const t of map.values()) t.buckets.sort((a, b) => a.le - b.le);
  return map;
}

/**
 * Estimates a quantile from cumulative histogram buckets by linear
 * interpolation inside the bucket that crosses the target rank (the same
 * approach as PromQL's histogram_quantile). Returns seconds.
 */
export function histogramQuantile(q: number, buckets: { le: number; count: number }[]): number | undefined {
  if (!buckets.length) return undefined;
  const total = buckets[buckets.length - 1].count;
  if (total === 0) return undefined;
  const rank = q * total;
  let prevLe = 0;
  let prevCount = 0;
  for (const b of buckets) {
    if (b.count >= rank) {
      if (b.le === Infinity) return prevLe; // can't interpolate into +Inf
      const inBucket = b.count - prevCount;
      const frac = inBucket === 0 ? 0 : (rank - prevCount) / inBucket;
      return prevLe + (b.le - prevLe) * frac;
    }
    prevLe = b.le;
    prevCount = b.count;
  }
  return prevLe;
}

/** Sums histograms across models (buckets share the same boundaries). */
export function mergeBuckets(list: ModelTraffic[]) {
  const byLe = new Map<number, number>();
  for (const t of list) for (const b of t.buckets) byLe.set(b.le, (byLe.get(b.le) ?? 0) + b.count);
  return [...byLe.entries()].map(([le, count]) => ({ le, count })).sort((a, b) => a.le - b.le);
}
