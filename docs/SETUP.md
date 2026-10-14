# Setup and development

## Prerequisites

| Tool | Version | Needed for |
|---|---|---|
| Docker Desktop | recent | running the whole stack |
| Go | 1.26 | backend development and tests |
| Python | 3.11+ | training, ml-service, scripts |
| Node.js | 20+ | dashboard development |
| `protoc` + Go/Python plugins | | only when you change `proto/predict.proto` |
| Git Bash (Windows) | | the `.sh` scripts |

## Run everything

```bash
cp .env.example .env                    # defaults work locally
docker compose up -d --build
bash scripts/seed_registry.sh           # registers loan_default_lr and loan_default_xgb v1
bash scripts/e2e_smoke.sh               # checks the full flow on a throwaway model
```

| Service | URL |
|---|---|
| Dashboard | http://localhost:3000 |
| Go API | http://localhost:8080 (`/healthz`, `/metrics`, `/api/v1/...`) |
| ml-service | gRPC `localhost:50051`, HTTP http://localhost:8000 |
| MinIO console | http://localhost:9001 (minioadmin / minioadmin) |
| Prometheus | http://localhost:9090 |
| Grafana | http://localhost:3001 (admin / `GRAFANA_ADMIN_PASSWORD`) |
| Postgres | `localhost:5432` (driftline / driftline) |

Reset all data: `docker compose down -v`.

## Environment variables

All in `.env` (see `.env.example`). Defaults let the server start with no `.env` at all.

| Variable | Default | Meaning |
|---|---|---|
| `API_KEY` | empty | Protects `/api/v1`. Empty = no auth. Scripts and dashboard read `DRIFTLINE_API_KEY` |
| `SLACK_WEBHOOK_URL`, `DISCORD_WEBHOOK_URL` | empty | Alert channels. Empty = alerts only shown on the dashboard |
| `ALERT_COOLDOWN` | 30m | Don't repeat the same alert inside this window |
| `DRIFT_ENABLED` | true | Run the drift scheduler |
| `DRIFT_INTERVAL` / `DRIFT_WINDOW` | 1m / 15m | How often, and over how much recent traffic |
| `DRIFT_MIN_SAMPLES` | 100 | Skip a check with fewer logged requests |
| `DRIFT_FEATURE_PSI` / `DRIFT_OVERALL_PSI` | 0.25 / 0.1 | Drift thresholds |
| `DRIFT_WORKERS` | 4 | Concurrent version checks |
| `AUTO_ROLLBACK` | true | false = alert only |
| `ROLLBACK_*` | see [ARCHITECTURE.md](ARCHITECTURE.md#rollback-rules) | Canary health thresholds |
| `PREDICTION_LOG_BUFFER` | 10000 | Async log channel size |
| `DATABASE_URL`, `ML_SERVICE_ADDR`, `MINIO_*`, `HTTP_ADDR` | localhost values | Set by compose inside containers |

## Working on one part

Run the infrastructure in Docker and the part you're changing on your machine.

**Go server**

```bash
docker compose up -d postgres minio ml-service
go run ./go/cmd/server
```

**ml-service**

```bash
cd ml-service
pip install -r requirements-dev.txt
python main.py
```

**Dashboard**

```bash
cd frontend
npm install
GO_API_URL=http://localhost:8080 ML_SERVICE_URL=http://localhost:8000 npm run dev
```

After changing frontend code, rebuild its container to see it on :3000: `docker compose up -d --build frontend`.

**Training**

```bash
cd ml/projects/loan_default
pip install -r requirements.txt
python train.py --version v2          # trains, evaluates, SHAP, baseline, ONNX, publishes to ml-service/models
```

Or, against a running stack, `python scripts/retrain.py --canary 10` trains the next version, registers it and deploys it as a 10% canary.

## Changing the protobuf contract

Edit `proto/predict.proto`, then regenerate both sides:

```bash
bash scripts/gen_proto.sh      # or: powershell scripts/gen_proto.ps1
```

Output goes to `proto/predict/` (Go) and `ml-service/proto/` (Python). Commit the generated files. Only add fields; never renumber existing ones.

## Tests

```bash
go test -race ./go/...                                        # Go unit tests
go test -tags integration -v ./go/test/integration/           # needs the stack running
pytest -q ml/projects/loan_default/tests                      # training pipeline
cd ml-service && pytest -q                                    # ml-service
cd frontend && npm run lint && npx tsc --noEmit               # dashboard
bash scripts/e2e_smoke.sh                                     # full flow via HTTP
```

CI (`.github/workflows/ci.yml`) runs all of the above on every push and PR.

## Windows notes

- `.gitattributes` forces LF line endings. If gofmt complains about every file, it's CRLF: `git add --renormalize .`
- Run Python with `PYTHONUTF8=1` so files are read/written as UTF-8 instead of cp1252.
- Use Git Bash for the `.sh` scripts.
- `minio/minio` is no longer published on Docker Hub; compose uses `chainguard/minio` pinned by digest.
