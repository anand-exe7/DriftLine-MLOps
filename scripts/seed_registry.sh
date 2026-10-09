#!/usr/bin/env bash
# Registers the trained loan-default models (v1) in the Go registry with their
# real test metrics, SHAP importance and training baseline, then promotes v1
# to production. Safe to re-run: existing models/versions are left as they are.
set -euo pipefail
cd "$(dirname "$0")/.."

API="${API:-http://localhost:8080/api/v1}"
PY="${PYTHON:-python}"
ART=ml/projects/loan_default/artifacts
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

register() {
  local name=$1 key=$2 desc=$3
  local bundle=ml-service/models/$name/v1

  # metrics.json = this model's evaluation metrics + its SHAP ranking
  "$PY" - "$ART" "$key" > "$TMP/$name-metrics.json" <<'EOF'
import json, sys
art, key = sys.argv[1], sys.argv[2]
metrics = json.load(open(f"{art}/evaluation_metrics.json"))[key]
metrics["shap_importance"] = json.load(open(f"{art}/shap_importance.json"))[key]
print(json.dumps(metrics))
EOF

  code=$(curl -s -o "$TMP/out" -w '%{http_code}' -X POST "$API/models" \
    -H 'content-type: application/json' -d "{\"name\":\"$name\",\"description\":\"$desc\"}")
  echo "  model $name: $code"

  code=$(curl -s -o "$TMP/out" -w '%{http_code}' -X POST "$API/models/$name/versions" \
    -F version=v1 \
    -F "model=@$bundle/model.onnx" \
    -F "feature_schema=@$bundle/feature_schema.json" \
    -F "metrics=@$TMP/$name-metrics.json" \
    -F "baseline_stats=@$ART/baseline_stats.json")
  echo "  version $name/v1: $code"
  [[ $code = 201 || $code = 409 ]] || { cat "$TMP/out"; exit 1; }

  code=$(curl -s -o "$TMP/out" -w '%{http_code}' -X POST "$API/models/$name/versions/v1/promote" \
    -H 'content-type: application/json' -d '{"stage":"production"}')
  echo "  promote $name/v1 -> production: $code"
}

echo "seeding $API"
register loan_default_lr logistic_regression "Logistic regression (scaled, class-balanced), loan default"
register loan_default_xgb xgboost "XGBoost (300 trees, class-balanced), loan default"
echo "done"
