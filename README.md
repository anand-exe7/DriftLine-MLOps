# Driftline — AI Assistant Context

This file exists so any AI assistant (Claude, Gemini, Copilot, etc.) working on this repo has full context without the user re-explaining everything each session. Read this fully before helping with any part of the project.

---

## What This Project Is

**Driftline** is a production-grade MLOps platform that manages the full lifecycle of ML models — registration, deployment, monitoring, drift detection, automated rollback, and retraining. It is NOT a research/ML-heavy project. The ML models used are intentionally simple (Logistic Regression + XGBoost on tabular data). The actual engineering difficulty and learning goal of this project is the **Go backend and systems design** around the model — drift detection math, canary deployments, gRPC communication, concurrency, and observability.

Team: 2 people. One Go/backend-focused, one splitting time between ML and Go.

---

## Core Problem This Solves

Most ML projects stop after deployment. In production, models degrade over time due to data drift (real-world data no longer matches training data) and concept drift. Driftline continuously monitors deployed models, detects this degradation statistically, and automatically reacts — alerting, rolling back to a previous good version, or triggering retraining — without manual intervention.

---

## Architecture Overview

```
Next.js Dashboard (REST, JSON)
        │
   Go Control Plane (REST API)
        │
┌───────────────────────────────┐
│  Model Registry                │
│  Deployment Manager            │
│  Prediction Proxy (canary)     │
│  Drift Scheduler               │
│  Alert Service                 │
│  Rollback Manager              │
└───────────────────────────────┘
        │
   gRPC (protobuf) ──────────────► Python ml-service (FastAPI + gRPC server)
        │                              │
   PostgreSQL + Redis            Loads ONNX models, runs inference
        │
   MinIO (model artifact storage)
        │
   Prometheus + Grafana + Loki (observability)
```

**Key architectural decision:** Go backend ↔ Python ml-service communicate via **gRPC + protobuf**, not REST/JSON. Reason: this is a high-frequency, latency-sensitive internal path (every prediction goes through it), and typed contracts prevent silent Go↔Python type mismatches. Frontend ↔ Go backend stays plain REST/JSON — no benefit to gRPC there (browser compatibility, not latency-critical).

---

## Tech Stack

**Backend (Go):** Chi/Gin, PostgreSQL, Redis, Docker SDK, gRPC, cron scheduler
**ML Training (Python):** pandas, scikit-learn, XGBoost, SHAP, skl2onnx/onnxmltools
**ML Serving (Python):** FastAPI + gRPC server, ONNX Runtime
**Infra:** Docker, Docker Compose, Prometheus, Grafana, Loki, MinIO, GitHub Actions
**Frontend:** Next.js, Tailwind CSS, shadcn/ui, Recharts
**Communication:** gRPC/protobuf (Go↔Python), REST/JSON (frontend↔Go)

---

## Repo Structure

```
driftline/
├── proto/                    # shared .proto definitions + generated Go/Python code
├── go/
│   ├── cmd/server/           # main.go entry point
│   └── internal/
│       ├── config/
│       ├── db/                # + migrations/
│       ├── models/             # Go structs
│       ├── registry/           # Model Registry module (handler/service/repository)
│       ├── deployment/         # Deployment Manager (Docker orchestration)
│       ├── prediction/         # Prediction Proxy (canary routing) + gRPC client
│       ├── drift/               # Drift Scheduler + PSI/KS-test detector
│       ├── alert/                # Alert Service (Slack/Discord/email)
│       ├── rollback/             # Rollback Manager
│       ├── storage/              # MinIO client
│       ├── worker/                # worker pool for async jobs
│       └── middleware/            # auth, logging, recovery, gRPC interceptors
├── ml/train/                  # training scripts (offline, run occasionally)
├── ml-service/                 # FastAPI + gRPC serving layer (always running)
├── frontend/                    # Next.js dashboard
└── docker-compose.yml
```

**Module pattern:** each Go module under `internal/` follows `handler.go` (HTTP/gRPC entry) → `service.go` (business logic) → `repository.go` (DB queries). Keep these separated — don't put DB queries in handlers or business logic in repositories.

---

## Database Schema (core tables)

`users`, `models`, `model_versions`, `deployments`, `datasets`, `prediction_logs`, `drift_reports`, `feature_statistics`, `performance_metrics`, `alerts`, `training_runs`

---

## ML Side — Current State

- **Dataset:** Telco Customer Churn (Kaggle), ~7,000 rows, binary classification (churn/no churn)
- **Models trained:** Logistic Regression AND XGBoost, trained side by side for comparison (not just one — this was a deliberate choice so the user learns both a fully-transparent linear model and a boosted-tree model)
- **Results so far:** Logistic Regression: Accuracy 80.4%, F1 0.61, ROC-AUC 0.836. XGBoost: Accuracy 79.2%, F1 0.58, ROC-AUC 0.837. (Logistic Regression slightly outperformed XGBoost on this dataset — a legitimate, non-fabricated result worth keeping as-is for the Model Comparison feature.)
- **Day 1 completed:** load, clean, encode, split, train both models, evaluate, save as `.pkl`/`.json`
- **Day 2 (in progress/next):** SHAP feature importance, save training data statistics (baseline for drift detection), export both models to ONNX, verify ONNX exports match original predictions
- **Serving format:** ONNX (portable, loaded by ml-service via ONNX Runtime — NOT scikit-learn/XGBoost directly at serving time)

---

## Go Concepts This Project Is Meant to Teach

The user is intentionally using this project to practice: gRPC + protobuf, goroutines/channels for the drift scheduler, `context.Context` propagation, worker pools (concurrency control — distinct from "warm pools," not needed here since ml-service runs continuously), `sync` package (mutex/atomic for canary traffic counters), interfaces for swappable implementations (e.g. `AlertNotifier`), custom error wrapping, gRPC interceptors, generics (optional, for repository patterns), structured logging (`slog`), table-driven tests (especially for PSI/KS-test drift math), and `time.Ticker`/cron scheduling.

When helping with Go code, prefer surfacing these patterns explicitly rather than writing the most "clever" or terse solution — the goal is the user understanding and being able to explain these concepts, not just working code.

---

## How the User Wants Help

- **Learning style:** documentation-first. The user is reading official docs (Go stdlib, gRPC, Postgres driver, etc.) themselves and using AI only for (a) explaining unfamiliar *concepts* before diving into docs, and (b) describing the *flow/structure* of a function in plain language or pseudocode — NOT full working code handed over to copy-paste. Let the user write the actual Go syntax themselves where reasonably possible.
- **Communication style:** direct, concise, plain-English. Explain the "what" and "why" together. Avoid padded or overly formal responses.
- **Scope honesty:** the user values being told clearly when something is "easy" vs genuinely hard, and where the real difficulty of this project actually lives (it's in the Go backend/systems design, not the ML models — keep reinforcing this framing, don't inflate the ML complexity).

---

## Current Status (update this section as the project progresses)

- [x] ML Day 1: data cleaning, train/test split, Logistic Regression + XGBoost trained and evaluated, models saved locally
- [x] ML Day 2: SHAP importance, training baseline stats, ONNX export + verification (dataset: Loan Default, not Telco)
- [x] Go: repo/proto scaffolding, DB migrations, Model Registry module
- [x] ml-service: Python gRPC PredictionService serving ONNX bundles
- [x] Go: MinIO storage wrapper, Alert notifiers (Slack/Discord)
- [x] docker compose end-to-end run (`scripts/e2e_smoke.sh`: registry -> MinIO -> ml-service -> prediction)
- [ ] Go: Deployment Manager, Prediction Proxy + gRPC client
- [ ] Go: Drift Scheduler + PSI/KS-test implementation
- [ ] Go: Alert Service, Rollback Manager
- [ ] Observability: Prometheus/Grafana/Loki wiring
- [ ] Frontend dashboard
- [ ] Retraining loop (closes the self-healing story)
- [ ] Demo prep: script to inject synthetic drift live

---

## Naming Note

Project was originally called "SentinelML" in early planning — it has been renamed to **Driftline**. Use this name everywhere.