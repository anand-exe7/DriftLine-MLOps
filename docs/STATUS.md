# Status and what's left

Last updated: 2026-10-14. Branch with the latest work: `feat/full-platform` (not merged into `main` yet).

## Done and verified

Everything below was run against the Docker stack, not just unit-tested.

| Area | State | Proof |
|---|---|---|
| Training pipeline (LR + XGBoost, SHAP, baseline, ONNX) | ✅ | ONNX parity check, `ml/projects/loan_default/tests` |
| ml-service (gRPC + ONNX Runtime, MinIO fallback, Load/Unload) | ✅ | 11 pytest tests |
| Proto contract (`PredictionService`) | ✅ | Go + Python generated |
| Model Registry (upload, stages, download, delete) | ✅ | Unit tests, integration test |
| Deployment Manager (primary + canary, traffic, promote, stop, rollback) | ✅ | Unit tests; live: 90/10 split gave exactly 675/75 |
| Prediction Proxy (routing, gRPC, async batched logging) | ✅ | Unit tests; live 400/404 paths |
| Drift Scheduler (PSI + KS, worker pool) | ✅ | Table-driven tests; live: normal traffic PSI 0.003, downturn flagged Income, InterestRate, DTIRatio, CreditScore |
| Rollback Manager (canary health + canary drift, retrain advice on primary drift) | ✅ | Unit tests; live rollback of canary and primary |
| Alerts (Slack, Discord, cooldown, history) | ✅ | Unit tests; dashboard shows history |
| API-key auth | ✅ | Middleware tests |
| Prometheus + Grafana | ✅ | Both targets up, dashboard shows live data |
| Dashboard (8 pages, live data only) | ✅ | Checked in the browser |
| Scripts: seed, retrain, simulate traffic (normal/drift) | ✅ | Used in the live checks above |
| Go integration test | ✅ | Passes against the stack |

## Finish before merging to `main`

- [ ] **Run `scripts/e2e_smoke.sh`.** It was rewritten; the `field()` bug is fixed but it hasn't been run since.
- [ ] **Rebuild the dashboard container** (`docker compose up -d --build frontend`) to pick up two small fixes: latency shows `<1 ms` instead of 0, and the primary is listed first.
- [ ] **Commit** the uncommitted work on `feat/full-platform`: Prometheus/Grafana, compose changes, CI workflow, Deployments and Data Drift pages, server-side API proxy, model delete endpoint, script updates, these docs.
- [ ] **Update the root `README.md`.** The checklist is out of date and the "ML side" section still describes the old Telco dataset. Point it at `docs/`.
- [ ] **Add a `Makefile`** with `up`, `down`, `seed`, `smoke`, `test`, `proto`, `demo-drift`.
- [ ] **Push and watch CI.** `.github/workflows/ci.yml` has never run on GitHub; expect small fixes.
- [ ] **Merge into `main`.**

## Next features (not started)

Ordered by value for the project. Sizes: S = under a day, M = a few days.

| # | Feature | Why | Size | Where |
|---|---|---|---|---|
| 1 | **Ground truth + real performance tracking**: `POST /api/v1/feedback` with the actual outcome for a `request_id`, then accuracy/recall per version over time | Today we only detect *data* drift. Labels let us detect *concept* drift (model getting worse at the same inputs). Fills the spec's `performance_metrics` table | M | new `go/internal/feedback`, migration 009, dashboard chart |
| 2 | **Automatic retraining trigger**: when a primary-drift recommendation fires, start `retrain.py` as a job and deploy the result as a canary | Closes the self-healing loop without a human. Fills `training_runs` | M | `go/internal/retrain` or a worker job + `training_runs` table |
| 3 | **Use Redis** (already in compose, nothing uses it yet): shared route cache + rate limiting on `/predict` | Needed as soon as there are two Go server replicas | S–M | `go/internal/prediction/router.go`, middleware |
| 4 | **Loki** for logs | The spec lists it; Grafana could show logs next to metrics | S | compose + `infra/` |
| 5 | **Users and login** instead of one shared API key | `users` table exists but isn't used | M | middleware, dashboard |
| 6 | **Email alert channel** | Spec mentions it; just another `AlertNotifier` implementation | S | `go/internal/alert` |
| 7 | **Dataset / feature statistics tables** from the spec (`datasets`, `feature_statistics`) | Track which data each version was trained on | S | migration + registry |
| 8 | **Load test**: proxy throughput and p99 with k6 or `hey` | Good numbers for the write-up and interviews | S | `scripts/` |

## Known gaps and design decisions to be aware of

- **The Deployment Manager doesn't start Docker containers** (the original spec said "Docker SDK"). Instead, one ml-service hosts many versions and the manager warms them with the `LoadModel` RPC. Faster and simpler for one machine; per-version containers would only matter for isolation at scale.
- **Primary drift does not roll back**, by design (see [ARCHITECTURE.md](ARCHITECTURE.md#rollback-rules)).
- **Prediction logs are dropped, not blocked,** if the buffer fills. Counted in the `driftline_prediction_logs_dropped_total` metric.
- **No real webhook configured yet.** Set `SLACK_WEBHOOK_URL` or `DISCORD_WEBHOOK_URL` in `.env` to get alerts outside the dashboard.
- **Single replica.** The route cache and canary counter are in-memory per server process (item 3 fixes this).
