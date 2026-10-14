# What Driftline is

Driftline is an MLOps platform that looks after machine-learning models **after** they are deployed.

Most ML projects stop once the model is serving. In production the world keeps changing: incomes shift, interest rates move, applicants look different from the people the model was trained on. The model doesn't crash; it just quietly gets worse. Driftline watches for that and reacts on its own:

1. **Register** every model version with its metrics, explanation (SHAP) and training data profile.
2. **Deploy** a new version safely: start it as a *canary* on a small slice of traffic next to the current *primary*.
3. **Serve** every prediction through one proxy that routes, times and logs it.
4. **Detect drift**: every minute, compare live inputs to the training data, feature by feature (PSI + KS test).
5. **React**: alert on Slack/Discord, roll an unhealthy canary back automatically, and recommend retraining when the primary drifts.
6. **Retrain** with one script that trains the next version and ships it as a canary, closing the loop.

The ML models are deliberately simple (Logistic Regression and XGBoost on tabular loan data). The hard part, and the point of the project, is the **Go backend and systems design** around them: gRPC, concurrency, canary routing, drift statistics, transactions and observability.

## The use case

**Loan default prediction.** Given an application (age, income, loan amount, credit score, months employed, interest rate, DTI ratio, education, employment type, ...), predict whether the borrower will default.

- Dataset: Kaggle *Loan Default*, ~255k rows, ~11.6% defaults (imbalanced, so both models use class weighting).
- Held-out test results (20%):

| Model | ROC-AUC | Recall (defaults caught) | Precision | F1 |
|---|---|---|---|---|
| Logistic Regression | 0.753 | 0.70 | 0.22 | 0.33 |
| XGBoost | 0.754 | 0.67 | 0.23 | 0.34 |

These are honest numbers for this dataset. Catching ~70% of defaults is the goal; low precision is the trade-off of class weighting.

## Components

```
                Next.js dashboard  (:3000)
                        │  REST / JSON (server-side proxy adds the API key)
                        ▼
   ┌──────────────── Go control plane (:8080) ────────────────┐
   │ Model Registry     Deployment Manager   Prediction Proxy │
   │ Drift Scheduler    Rollback Manager     Alert Service    │
   └───────┬───────────────────┬────────────────────┬─────────┘
           │ gRPC + protobuf   │ SQL                │ S3 API
           ▼                   ▼                    ▼
   Python ml-service      PostgreSQL 17          MinIO
   ONNX Runtime (:50051)  registry, logs,        model artifacts
   + HTTP side port :8000 drift reports, alerts  (.onnx bundles)

   Prometheus (:9090) scrapes Go + ml-service ──► Grafana (:3001)
```

| Component | Where | What it does |
|---|---|---|
| Training pipeline | `ml/projects/loan_default/` | Clean data, train LR + XGBoost, evaluate, SHAP, training baseline (decile bins per feature), export to ONNX with a parity check |
| ml-service | `ml-service/` | Python gRPC server running ONNX Runtime. Validates features, caches one session per version, loads/unloads on demand, pulls missing versions from MinIO |
| Proto contract | `proto/predict.proto` | `PredictionService`: `Predict`, `HealthCheck`, `LoadModel`, `UnloadModel` |
| Model Registry | `go/internal/registry` | Create models, upload versions (streamed to MinIO with SHA-256), stage changes, download, delete |
| Deployment Manager | `go/internal/deployment` | Primary + canary deployments, traffic split, promote, stop, rollback. Warms the model before it gets traffic |
| Prediction Proxy | `go/internal/prediction` | `POST /predict/{model}`: weighted routing, gRPC call, async batched logging to Postgres |
| Drift Scheduler | `go/internal/drift` | PSI + binned KS per feature vs the training baseline, run on a ticker through a worker pool |
| Rollback Manager | `go/internal/rollback` | Rolls back canaries that error or behave differently from the primary, or that drift alone |
| Alert Service | `go/internal/alert` | Slack + Discord notifiers behind one interface, cooldown, every attempt recorded |
| Observability | `go/internal/metrics`, `infra/` | Prometheus metrics from both services, provisioned Grafana dashboard |
| Dashboard | `frontend/` | Next.js UI: overview, registry, deployments, prediction lab, monitoring, data drift, alerts, guide |
| Scripts | `scripts/` | Seed the registry, retrain, simulate (drifted) traffic, end-to-end smoke test, protobuf generation |

## Why these choices

- **ONNX for serving**: one portable file per model, so serving doesn't need scikit-learn or XGBoost. Export is verified (max difference ~6e-7 vs the original).
- **gRPC between Go and Python**: every prediction crosses this hop, so it should be fast and typed. Browsers talk plain REST.
- **Go for the control plane**: goroutines, channels and `context` fit the scheduler, worker pool and async log writer; the standard library covers HTTP well.
- **Postgres for everything stateful**: deployments, logs, drift reports and alerts live in one transactional store, so traffic changes are atomic.
- **MinIO**: S3-compatible artifact storage that runs locally; swapping to real S3 is a config change.
