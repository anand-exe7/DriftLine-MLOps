#!/usr/bin/env bash
# End-to-end smoke test against a running `docker compose up` stack:
#   Go registry -> MinIO -> ml-service (MinIO fallback) -> prediction.
# Registers the bundled LR model under a fresh version that is NOT baked into
# the ml-service image, so the prediction only works if the whole chain does.
set -euo pipefail
cd "$(dirname "$0")/.."

API="${API:-http://localhost:8080/api/v1}"
ML="${ML:-http://localhost:8000}"
MODEL=loan_default_lr
VERSION="e2e-$(date +%s)"
BUNDLE=ml-service/models/loan_default_lr/v1
PY="${PYTHON:-python}"

pass() { printf '  \033[32mok\033[0m  %s\n' "$1"; }
fail() { printf '  \033[31mFAIL\033[0m %s\n' "$1"; exit 1; }

# status code of a request, body saved to $TMP
TMP=$(mktemp)
trap 'rm -f "$TMP"' EXIT
req() { curl -s -o "$TMP" -w '%{http_code}' "$@"; }

echo "1. health"
[ "$(req "${API%/api/v1}/healthz")" = 200 ] && pass "server /healthz" || fail "server unhealthy: $(cat "$TMP")"
[ "$(req "$ML/health")" = 200 ] && pass "ml-service /health" || fail "ml-service unhealthy"

echo "2. registry"
code=$(req -X POST "$API/models" -H 'content-type: application/json' \
  -d "{\"name\":\"$MODEL\",\"description\":\"loan default logistic regression\"}")
[[ "$code" = 201 || "$code" = 409 ]] && pass "create model ($code)" || fail "create model: $code $(cat "$TMP")"

code=$(req -X POST "$API/models/$MODEL/versions" \
  -F "version=$VERSION" \
  -F "model=@$BUNDLE/model.onnx" \
  -F "feature_schema=@$BUNDLE/feature_schema.json" \
  -F "baseline_stats=@ml/projects/loan_default/artifacts/baseline_stats.json")
[ "$code" = 201 ] && pass "upload $MODEL/$VERSION" || fail "upload: $code $(cat "$TMP")"
checksum=$("$PY" -c "import json,sys; print(json.load(open(sys.argv[1]))['checksum_sha256'])" "$TMP")

code=$(req -X POST "$API/models/$MODEL/versions/$VERSION/promote" -H 'content-type: application/json' -d '{"stage":"production"}')
[ "$code" = 200 ] && grep -q '"stage":"production"' "$TMP" && pass "promote to production" || fail "promote: $code $(cat "$TMP")"

code=$(req -X POST "$API/models/$MODEL/versions/$VERSION/promote" -H 'content-type: application/json' -d '{"stage":"bogus"}')
[ "$code" = 400 ] && pass "invalid stage rejected (400)" || fail "invalid stage: $code"

[ "$(req "$API/models/$MODEL/versions/$VERSION/artifact")" = 200 ] || fail "download artifact"
got=$("$PY" -c "import hashlib,sys; print(hashlib.sha256(open(sys.argv[1],'rb').read()).hexdigest())" "$TMP")
[ "$got" = "$checksum" ] && pass "downloaded artifact matches sha256" || fail "checksum mismatch $got != $checksum"

prod=$(curl -s "$API/models/$MODEL/versions" | "$PY" -c "import json,sys; print(sum(v['stage']=='production' for v in json.load(sys.stdin)))")
[ "$prod" = 1 ] && pass "exactly one production version" || fail "$prod production versions"

echo "3. serving (ml-service pulls $VERSION from MinIO)"
features=$("$PY" -c "
import json,sys
names = json.load(open(sys.argv[1]))['features']
f = {n: 0.0 for n in names}
f.update(Age=32, Income=45000, LoanAmount=150000, CreditScore=420, MonthsEmployed=6,
         NumCreditLines=3, InterestRate=21.5, LoanTerm=48, DTIRatio=0.7)
print(json.dumps(f))" "$BUNDLE/feature_schema.json")

code=$(req -X POST "$ML/predict" -H 'content-type: application/json' \
  -d "{\"model_name\":\"$MODEL\",\"model_version\":\"$VERSION\",\"features\":$features,\"request_id\":\"e2e\"}")
[ "$code" = 200 ] && pass "predict: $(cat "$TMP")" || fail "predict: $code $(cat "$TMP")"

code=$(req -X POST "$ML/predict" -H 'content-type: application/json' \
  -d "{\"model_name\":\"$MODEL\",\"model_version\":\"does-not-exist\",\"features\":$features}")
[ "$code" = 404 ] && pass "unknown version -> 404" || fail "unknown version: $code"

echo "all checks passed"
