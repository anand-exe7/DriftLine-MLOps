"use client";

import { go, ml, type Model, type ModelVersion } from "./api";
import { parseProm, trafficByModel } from "./prom";
import { usePoll } from "./usePoll";

export interface RegistryEntry {
  model: Model;
  versions: ModelVersion[];
  production?: ModelVersion;
}

/** Every model with its versions, newest first. */
export function useRegistry(intervalMs = 10000) {
  return usePoll<RegistryEntry[]>(async () => {
    const models = await go.models();
    return Promise.all(
      models.map(async (model) => {
        const versions = await go.versions(model.name);
        return { model, versions, production: versions.find((v) => v.stage === "production") };
      }),
    );
  }, intervalMs);
}

export interface SystemHealth {
  go: boolean;
  postgres: boolean;
  minio: boolean;
  ml: boolean;
  mlLoaded: string[];
  details: Record<string, string>;
}

export function useSystemHealth(intervalMs = 5000) {
  return usePoll<SystemHealth>(async () => {
    const [g, m] = await Promise.allSettled([go.healthz(), ml.health()]);
    const details: Record<string, string> = {};
    let pg = false;
    let minio = false;
    let goUp = false;
    if (g.status === "fulfilled") {
      goUp = true;
      pg = g.value.postgres === "ok";
      minio = g.value.minio === "ok";
      if (!pg) details.postgres = g.value.postgres;
      if (!minio) details.minio = g.value.minio;
    } else {
      details.go = g.reason instanceof Error ? g.reason.message : String(g.reason);
    }
    return {
      go: goUp,
      postgres: pg,
      minio,
      ml: m.status === "fulfilled" && m.value.status === "ok",
      mlLoaded: m.status === "fulfilled" ? m.value.loaded_models : [],
      details,
    };
  }, intervalMs);
}

/** ml-service Prometheus counters, grouped by model/version. */
export function useTraffic(intervalMs = 3000) {
  return usePoll(async () => trafficByModel(parseProm(await ml.metricsText())), intervalMs);
}
