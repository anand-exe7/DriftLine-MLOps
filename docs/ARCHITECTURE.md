# Architecture

How each part works and the rules it keeps. Read [OVERVIEW.md](OVERVIEW.md) first.

## Go code layout

```
go/
├── cmd/server/main.go        wiring, background loops, graceful shutdown
├── internal/
│   ├── config/               env vars -> Config (collects all errors, fails once)
│   ├── db/                   pgx pool + embedded migrations (advisory lock)
│   ├── models/               shared structs
│   ├── registry/             handler -> service -> repository
│   ├── deployment/           handler -> service -> repository
│   ├── prediction/           router, service, async log writer, handler
│   ├── drift/                detector (math), scheduler, repository, handler
│   ├── rollback/             health judge + loop
│   ├── alert/                notifiers (Slack, Discord), service, handler
│   ├── mlclient/             gRPC client to ml-service (interface + impl)
│   ├── storage/              MinIO wrapper
│   ├── middleware/           API key auth, request logging/metrics, gRPC interceptors
│   ├── metrics/              all Prometheus collectors
│   └── worker/               generic worker pool Run[J, R]
└── test/integration/         full-stack test (build tag `integration`)
```

Every module follows **handler** (HTTP) → **service** (business rules) → **repository** (SQL). Errors are sentinel values (`ErrNotFound`, `ErrConflict`, `ErrInvalid`, ...) wrapped with context and checked with `errors.Is`; handlers map them to HTTP status codes.

## A prediction, end to end

1. Client calls `POST /api/v1/predict/{model}` with `{"features": {...}}`.
2. API-key middleware checks `X-API-Key` (or `Authorization: Bearer`) with a constant-time compare. Empty `API_KEY` = auth off.
3. The **router** looks up the routing table for that model (cached; invalidated on any deployment change). Routes come from running deployments; if none exist it falls back to the registry's `production` version at 100%.
4. The router picks a version:
   - Without a routing key: an atomic counter `n`, slot `(n*37) % 100`, compared against cumulative weights. Multiplying by 37 (coprime with 100) spreads canary hits evenly instead of in a block, and the split is **exact** over every 100 requests (90/10 gives exactly 10 canary hits).
   - With a routing key (sticky users): FNV hash of the key instead of the counter.
5. The service calls ml-service over gRPC (`Predict`) with a timeout from the interceptor chain.
6. ml-service validates features against the version's `feature_schema.json`, runs ONNX Runtime and returns prediction + probabilities + latency.
7. The proxy pushes a log row onto a **buffered channel** and returns immediately. A background writer drains it and inserts in batches with `COPY`. If the buffer is full the row is dropped and counted in a metric, so the database can never slow predictions down. On shutdown the writer drains what is left.

## Deployments (canary / blue-green)

A deployment row = one version of one model, with `role` (`primary` / `canary`), `status` (`running`, `stopped`, `rolled_back`, ...) and `traffic_percent`.

**Invariants**, enforced in the service and applied in **one transaction**:

- At most one running primary and one running canary per model.
- Running traffic always sums to 100.
- A per-model mutex serialises releases so two requests can't race.
- Before a version gets traffic, the manager calls `LoadModel` on ml-service (warm-up). If that fails, nothing changes.

Operations:

| Operation | Effect |
|---|---|
| Deploy (traffic 100 or no primary) | Becomes primary. The old primary is stopped (blue/green switch) |
| Deploy (traffic < 100) | Becomes canary with N%, primary gets 100-N |
| SetTraffic | Changes the canary share |
| Promote | Canary becomes primary at 100%, old primary stopped, registry stage set to `production` |
| Rollback | Running canary → `rolled_back`, primary back to 100%. With no canary, restores the previous primary |
| Stop | Stops one deployment; the other gets 100% |

After each change the route cache is invalidated and the registry stage is synced.

## Drift detection

**Baseline**: at training time `baseline.py` stores, per feature, the decile bin edges and the share of training rows in each bin (`baseline_stats.json`, uploaded with every version).

**Every `DRIFT_INTERVAL` (default 1m)** the scheduler, for each serving version:

1. Loads the logged inputs from the last `DRIFT_WINDOW` (default 15m). Skips if fewer than `DRIFT_MIN_SAMPLES` (default 100).
2. Bins them with the **same edges** (right-closed bins, identical to numpy's `searchsorted(side="left")` used in training).
3. Computes per feature:
   - **PSI** = Σ (actual − expected) × ln(actual / expected), with epsilon 1e-4 for empty bins.
   - **Binned KS**: max gap between the cumulative distributions, p-value from the Kolmogorov asymptotic formula.
4. A feature is drifted if PSI ≥ `DRIFT_FEATURE_PSI` (0.25). The version is drifted if any feature is, or mean PSI ≥ `DRIFT_OVERALL_PSI` (0.1).
5. Saves a `drift_reports` row and, if drifted, sends an alert and notifies the rollback manager.

Versions are checked concurrently through the generic worker pool (`DRIFT_WORKERS`).

PSI rule of thumb: < 0.1 stable, 0.1–0.25 moderate, > 0.25 significant.

## Rollback rules

The rollback manager runs every `ROLLBACK_CHECK_INTERVAL` (30s) and after every drift run.

A **canary** is rolled back (if `AUTO_ROLLBACK=true`, otherwise just alerted) when, over the last `ROLLBACK_WINDOW` with at least `ROLLBACK_MIN_REQUESTS`:

- its error rate > `ROLLBACK_MAX_ERROR_RATE` (5%), or exceeds the primary's by `ROLLBACK_MAX_ERROR_DELTA` (3 points), or
- its predicted-default rate differs from the primary's by more than `ROLLBACK_MAX_POSITIVE_DIFF` (25 points), or
- it drifts while the primary does not (the problem is the canary, not the data).

**Primary drift is never rolled back.** An older model sees the same new data and would be just as wrong. Instead Driftline sends a *retraining recommendation*; `scripts/retrain.py` is the response.

## ml-service

- `main.py` starts gRPC on :50051 and FastAPI on :8000 (`/health`, `/models`, `/metrics`, `/predict` for the dashboard).
- `model_store.py`: one ONNX session per `model/version`, created lazily under a lock, removable with `unload()`.
- `fetcher.py`: if a version isn't on local disk, download the bundle from MinIO.
- Bundle layout: `models/{model}/{version}/model.onnx` + `feature_schema.json`.

## Database

Migrations live in `go/internal/db/migrations/` and run automatically at startup (guarded by a Postgres advisory lock so two servers can't race).

| Migration | Tables |
|---|---|
| 001 | `users` |
| 002 | `models` |
| 003 | `model_versions` (stage, metrics, SHAP, baseline, artifact key, sha256) |
| 004 | `deployments` |
| 005 | `prediction_logs` (inputs as JSONB, version, latency, status) |
| 006 | `drift_reports` (per-feature PSI/KS as JSONB) |
| 007 | `alerts` (every notification attempt) |
| 008 | `deployments.role`, `deployments.status_reason` |

To change the schema, add `009_*.sql`. Never edit an applied migration.

## Observability

- Go `/metrics`: HTTP requests by **route pattern** (not raw path, so cardinality stays bounded), proxy predictions by model/version/role/outcome, gRPC latency, log buffer drops, drift PSI per feature, rollbacks.
- ml-service `/metrics` on :8000: inference counts and latency per model/version.
- Prometheus config: `infra/prometheus/prometheus.yml`. Grafana datasource and the "Driftline" dashboard are provisioned from `infra/grafana/`.

## Dashboard

Next.js App Router in `frontend/`. The browser never sees the API key: `src/app/go/[...path]/route.ts` and `src/app/ml/[...path]/route.ts` are server-side proxies that forward to the Go API / ml-service and attach `DRIFTLINE_API_KEY`. Pages poll for live data (no mock data anywhere).
