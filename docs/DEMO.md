# Demo script

About 10 minutes. Shows the whole self-healing story with real data. Keep the dashboard (http://localhost:3000) and Grafana (http://localhost:3001) open side by side.

## 0. Start clean

```bash
docker compose down -v
docker compose up -d --build
bash scripts/seed_registry.sh
```

Show **Overview** (all services green) and **Model Registry** (two models, metrics, SHAP, baseline).

## 1. Deploy and serve

Deploy XGBoost v1 as primary, from the **Deployments** page or:

```bash
curl -s -X POST localhost:8080/api/v1/deployments -H 'content-type: application/json' \
  -d '{"model":"loan_default_xgb","version":"v1"}'
```

Send normal traffic (real rows from the dataset):

```bash
python scripts/simulate_traffic.py --model loan_default_xgb --mode normal --rps 20 --duration 90 --check
```

Show the **Prediction Lab** in *Live routing* mode, **Live Monitoring**, and the Grafana request panels. The drift check prints PSI around 0.003: stable.

## 2. Ship a new version as a canary

```bash
python scripts/retrain.py --models loan_default_xgb --canary 20
```

On **Deployments** the split bar shows 80/20. Send traffic again and point out the split is exact.

Then either promote it (Promote button) or roll it back (Rollback button) and show the routes return to 100%.

## 3. Inject drift

Simulate an economic downturn (lower incomes, higher rates, worse credit, higher DTI):

```bash
python scripts/simulate_traffic.py --model loan_default_xgb --mode drift --rps 20 --duration 90 --check
```

On **Data Drift**: Income, InterestRate, DTIRatio and CreditScore turn red (PSI > 0.25, KS p < 0.0001); unrelated features stay green. An alert appears on **Alerts** (and in Slack/Discord if a webhook is set).

Because it's the **primary** that drifted, Driftline recommends retraining instead of rolling back. Explain why: an older model would be just as wrong on the new data.

## 4. Close the loop

```bash
python scripts/retrain.py --models loan_default_xgb --canary 10
```

The new version ships as a canary; if it misbehaves, the rollback manager removes it automatically.

## Talking points

- Exact canary split from an atomic counter (no randomness).
- Logging never slows predictions: buffered channel + batched `COPY`.
- Traffic changes are one transaction; shares always sum to 100.
- Primary drift → retrain, canary problems → rollback.
- Same binning in Python training and Go detection, so PSI is comparable.
