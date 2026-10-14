# API reference

Base URL: `http://localhost:8080`. Everything under `/api/v1` needs `X-API-Key: <key>` when `API_KEY` is set.

Errors are JSON `{"error": "..."}` with: 400 invalid input, 404 not found, 409 conflict, 503 ml-service unavailable.

## Health and metrics

| Method | Path | Notes |
|---|---|---|
| GET | `/healthz` | Postgres, MinIO and ml-service status. 200 when all are ok |
| GET | `/metrics` | Prometheus |

## Model registry

| Method | Path | Notes |
|---|---|---|
| POST | `/api/v1/models` | `{"name": "loan_default_xgb", "description": "..."}` → 201 |
| GET | `/api/v1/models` | List models |
| GET | `/api/v1/models/{name}` | One model |
| DELETE | `/api/v1/models/{name}` | 204. 409 while any version has a running deployment. Deletes artifacts too |
| POST | `/api/v1/models/{name}/versions` | Multipart: `version`, `model` (.onnx), `feature_schema`, optional `metrics` (incl. SHAP), `baseline_stats` → 201 with `checksum_sha256` |
| GET | `/api/v1/models/{name}/versions` | List versions |
| GET | `/api/v1/models/{name}/versions/{version}` | One version |
| POST | `/api/v1/models/{name}/versions/{version}/promote` | `{"stage": "staging" \| "production" \| "archived"}` |
| GET | `/api/v1/models/{name}/versions/{version}/artifact` | Download the .onnx |

## Deployments

| Method | Path | Notes |
|---|---|---|
| POST | `/api/v1/deployments` | `{"model", "version", "traffic_percent"}`. 100 or omitted = primary, less = canary. 409 if that version already runs |
| GET | `/api/v1/deployments?model=` | List |
| GET | `/api/v1/deployments/{id}` | One |
| PATCH | `/api/v1/deployments/{id}/traffic` | `{"traffic_percent": 25}` (canary only) |
| POST | `/api/v1/deployments/{id}/promote` | Canary → primary |
| POST | `/api/v1/deployments/{id}/stop` | Stop it |
| POST | `/api/v1/deployments/rollback` | `{"model", "reason"}`: remove the canary, or restore the previous primary |

## Predictions

| Method | Path | Notes |
|---|---|---|
| POST | `/api/v1/predict/{model}` | `{"features": {...}, "routing_key": "optional"}` → `{prediction, probabilities, version, role, inference_ms, request_id}` |
| GET | `/api/v1/predict/{model}/routes` | Current routing table |
| GET | `/api/v1/predict/{model}/stats?window=15m` | Per-version requests, errors, latency, predicted-default rate |

Example:

```bash
curl -s -X POST localhost:8080/api/v1/predict/loan_default_xgb \
  -H 'content-type: application/json' \
  -d '{"features": {"Age": 32, "Income": 45000, "LoanAmount": 150000, "CreditScore": 420, "MonthsEmployed": 6, "NumCreditLines": 3, "InterestRate": 21.5, "LoanTerm": 48, "DTIRatio": 0.7}}'
```

Missing one-hot columns must be sent as 0; the full list is in each version's `feature_schema.json` (24 features).

## Drift

| Method | Path | Notes |
|---|---|---|
| GET | `/api/v1/drift/reports?model=&limit=` | Latest reports with per-feature PSI / KS |
| GET | `/api/v1/drift/config` | Active thresholds and intervals |
| POST | `/api/v1/drift/run` | Run a check now (otherwise every `DRIFT_INTERVAL`) |

## Alerts

| Method | Path | Notes |
|---|---|---|
| GET | `/api/v1/alerts` | Recent alert attempts (incl. failed sends) |
| GET | `/api/v1/alerts/channels` | Which channels are configured |
| POST | `/api/v1/alerts/test` | Send a test alert |

## ml-service

gRPC `PredictionService` on :50051 (`proto/predict.proto`):

| RPC | Purpose |
|---|---|
| `Predict(PredictRequest)` | Run one version on one feature vector |
| `HealthCheck` | Liveness + loaded models |
| `LoadModel(ModelRef)` | Warm a version (download from MinIO if needed) |
| `UnloadModel(ModelRef)` | Drop it from memory |

HTTP side port :8000 for the dashboard: `GET /health`, `GET /models`, `GET /metrics`, `POST /predict`.
