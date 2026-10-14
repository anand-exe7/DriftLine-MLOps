#!/usr/bin/env bash
# End-to-end smoke test against a running `docker compose up` stack. Uses its
# own throwaway model (deleted at the end), so it never disturbs real ones:
#
#   registry -> MinIO -> deploy (ml-service pulls from MinIO) -> canary
#   -> predictions through the Go proxy -> logs -> rollback -> delete
#
# Set DRIFTLINE_API_KEY if the server has API_KEY configured.
set -euo pipefail
cd "$(dirname "$0")/.."

BASE="${BASE:-http://localhost:8080}"
API="$BASE/api/v1"
MODEL="smoke_$(date +%s)"
BUNDLE=ml-service/models/loan_default_lr/v1
PY="${PYTHON:-python}"
AUTH=()
[ -n "${DRIFTLINE_API_KEY:-}" ] && AUTH=(-H "X-API-Key: $DRIFTLINE_API_KEY")

pass() { printf '  \033[32mok\033[0m  %s\n' "$1"; }
fail() { printf '  \033[31mFAIL\033[0m %s\n' "$1"; exit 1; }

TMP=$(mktemp)
cleanup() {
  # stop whatever is still running, then delete the model and its artifacts
  for id in $(curl -s "${AUTH[@]}" "$API/deployments?model=$MODEL" | "$PY" -c \
      "import json,sys; print(' '.join(d['id'] for d in json.load(sys.stdin) if d['status']=='running' and d['role']=='canary'))" 2>/dev/null); do
    curl -s -o /dev/null "${AUTH[@]}" -X POST "$API/deployments/$id/stop"
  done
  for id in $(curl -s "${AUTH[@]}" "$API/deployments?model=$MODEL" | "$PY" -c \
      "import json,sys; print(' '.join(d['id'] for d in json.load(sys.stdin) if d['status']=='running'))" 2>/dev/null); do
    curl -s -o /dev/null "${AUTH[@]}" -X POST "$API/deployments/$id/stop"
  done
  curl -s -o /dev/null "${AUTH[@]}" -X DELETE "$API/models/$MODEL" || true
  rm -f "$TMP"
}
trap cleanup EXIT

req() { curl -s -o "$TMP" -w '%{http_code}' "${AUTH[@]}" "$@"; }
field() { "$PY" -c "import json,sys; d=json.load(open(sys.argv[1])); print($1)" "$TMP"; }
jpost() { req -X "$1" "$2" -H 'content-type: application/json' -d "$3"; }

echo "1. health"
[ "$(req "$BASE/healthz")" = 200 ] && pass "control plane, postgres, minio" || fail "unhealthy: $(cat "$TMP")"
[ "$(field "d['ml_service']")" = ok ] && pass "ml-service reachable over gRPC" || fail "ml-service: $(cat "$TMP")"

echo "2. registry"
[ "$(jpost POST "$API/models" "{\"name\":\"$MODEL\"}")" = 201 ] && pass "create $MODEL" || fail "create: $(cat "$TMP")"
for v in v1 v2; do
  code=$(req -X POST "$API/models/$MODEL/versions" -F "version=$v" \
    -F "model=@$BUNDLE/model.onnx" -F "feature_schema=@$BUNDLE/feature_schema.json" \
    -F "baseline_stats=@ml/projects/loan_default/artifacts/baseline_stats.json")
  [ "$code" = 201 ] && pass "upload $v" || fail "upload $v: $code $(cat "$TMP")"
done
checksum=$(field "d['checksum_sha256']")
[ "$(req "$API/models/$MODEL/versions/v2/artifact")" = 200 ] || fail "download"
got=$("$PY" -c "import hashlib,sys; print(hashlib.sha256(open(sys.argv[1],'rb').read()).hexdigest())" "$TMP")
[ "$got" = "$checksum" ] && pass "downloaded artifact matches sha256" || fail "checksum mismatch"

echo "3. deployments (ml-service pulls the new model from MinIO)"
[ "$(jpost POST "$API/deployments" "{\"model\":\"$MODEL\",\"version\":\"v1\"}")" = 201 ] && pass "v1 primary at 100%" || fail "deploy v1: $(cat "$TMP")"
[ "$(jpost POST "$API/deployments" "{\"model\":\"$MODEL\",\"version\":\"v2\",\"traffic_percent\":20}")" = 201 ] && pass "v2 canary at 20%" || fail "deploy v2: $(cat "$TMP")"
[ "$(jpost POST "$API/deployments" "{\"model\":\"$MODEL\",\"version\":\"v2\",\"traffic_percent\":10}")" = 409 ] && pass "duplicate deploy rejected (409)" || fail "duplicate allowed"

echo "4. predictions through the proxy"
FEATURES=$("$PY" -c "
import json,sys
names = json.load(open(sys.argv[1]))['features']
f = {n: 0.0 for n in names}
f.update(Age=32, Income=45000, LoanAmount=150000, CreditScore=420, MonthsEmployed=6,
         NumCreditLines=3, InterestRate=21.5, LoanTerm=48, DTIRatio=0.7)
print(json.dumps(f))" "$BUNDLE/feature_schema.json")
v1=0; v2=0
for _ in $(seq 1 50); do
  [ "$(jpost POST "$API/predict/$MODEL" "{\"features\":$FEATURES}")" = 200 ] || fail "predict: $(cat "$TMP")"
  case "$(field "d['version']")" in v1) v1=$((v1+1));; v2) v2=$((v2+1));; esac
done
[ "$v1" = 40 ] && [ "$v2" = 10 ] && pass "50 predictions split exactly 40/10" || fail "split was $v1/$v2"
[ "$(jpost POST "$API/predict/$MODEL" '{"features":{"Age":1}}')" = 400 ] && pass "invalid features -> 400" || fail "invalid features"
sleep 2 # let the async log writer flush
[ "$(req "$API/predict/$MODEL/stats?window=10m")" = 200 ] || fail "stats"
logged=$(field "sum(v['requests'] for v in d['versions'])")
[ "$logged" = 51 ] && pass "all 51 requests logged to prediction_logs" || fail "logged $logged of 51"

echo "5. rollback + delete"
[ "$(jpost POST "$API/deployments/rollback" "{\"model\":\"$MODEL\",\"reason\":\"smoke test\"}")" = 200 ] && pass "canary rolled back" || fail "rollback: $(cat "$TMP")"
req "$API/predict/$MODEL/routes" >/dev/null
[ "$(field "[(r['version'], r['weight']) for r in d]")" = "[('v1', 100)]" ] && pass "v1 back at 100%" || fail "routes: $(cat "$TMP")"
[ "$(req -X DELETE "$API/models/$MODEL")" = 409 ] && pass "delete refused while deployed (409)" || fail "delete allowed while running"

echo "all checks passed"
