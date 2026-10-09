import json
import uuid
from concurrent import futures
from pathlib import Path

import grpc
import pytest
from fastapi.testclient import TestClient

from app.http_api import create_http_app
from app.model_store import ModelStore
from app.servicer import INVALID_ARGUMENT, NOT_FOUND, OK, PredictionServicer
from main import build_grpc_server
from proto import predict_pb2, predict_pb2_grpc

MODELS = Path(__file__).resolve().parents[1] / "models"
SCHEMA = json.loads((MODELS / "loan_default_xgb" / "v1" / "feature_schema.json").read_text())

# A plausible applicant: continuous features filled, every one-hot flag off.
APPLICANT = {name: 0.0 for name in SCHEMA["features"]}
APPLICANT.update({
    "Age": 32, "Income": 45000, "LoanAmount": 150000, "CreditScore": 420,
    "MonthsEmployed": 6, "NumCreditLines": 3, "InterestRate": 21.5,
    "LoanTerm": 48, "DTIRatio": 0.7,
})


@pytest.fixture(scope="module")
def store():
    return ModelStore(MODELS)


@pytest.fixture(scope="module")
def stub(store):
    server, port = build_grpc_server(PredictionServicer(store), port=0, workers=4)
    server.start()
    channel = grpc.insecure_channel(f"localhost:{port}")
    yield predict_pb2_grpc.PredictionServiceStub(channel)
    channel.close()
    server.stop(grace=None)


def request(model="loan_default_xgb", version="v1", features=None):
    return predict_pb2.PredictionRequest(
        model_name=model, model_version=version,
        features=APPLICANT if features is None else features,
        request_id=str(uuid.uuid4()),
    )


@pytest.mark.parametrize("model", ["loan_default_lr", "loan_default_xgb"])
def test_predict_ok(stub, model):
    req = request(model)
    resp = stub.Predict(req, timeout=5)
    assert resp.status_code == OK, resp.error_message
    assert resp.request_id == req.request_id
    assert resp.model_version_used == "v1"
    assert resp.prediction in (0, 1)
    assert len(resp.probabilities) == 2
    assert sum(resp.probabilities) == pytest.approx(1.0, abs=1e-5)
    assert resp.prediction == max(range(2), key=lambda i: resp.probabilities[i])
    assert resp.latency_ms >= 0


def test_missing_feature_is_invalid_argument(stub):
    features = dict(APPLICANT)
    features.pop("CreditScore")
    resp = stub.Predict(request(features=features), timeout=5)
    assert resp.status_code == INVALID_ARGUMENT
    assert "CreditScore" in resp.error_message


def test_unknown_feature_is_invalid_argument(stub):
    resp = stub.Predict(request(features={**APPLICANT, "ShoeSize": 42}), timeout=5)
    assert resp.status_code == INVALID_ARGUMENT
    assert "ShoeSize" in resp.error_message


@pytest.mark.parametrize("model,version", [("nope", "v1"), ("loan_default_xgb", "v99"), ("..", "..")])
def test_unknown_model_is_not_found(stub, model, version):
    resp = stub.Predict(request(model, version), timeout=5)
    assert resp.status_code == NOT_FOUND


def test_concurrent_requests_share_one_session(stub, store):
    with futures.ThreadPoolExecutor(16) as pool:
        results = list(pool.map(lambda _: stub.Predict(request(), timeout=5), range(64)))
    assert all(r.status_code == OK for r in results)
    assert len({tuple(r.probabilities) for r in results}) == 1  # deterministic
    assert store.loaded().count("loan_default_xgb/v1") == 1


def test_health_reports_loaded_models(stub):
    stub.Predict(request("loan_default_lr"), timeout=5)
    resp = stub.HealthCheck(predict_pb2.HealthRequest(), timeout=5)
    assert resp.status == "SERVING"
    assert "loan_default_lr/v1" in resp.loaded_models


def test_http_predict_matches_grpc(stub, store):
    client = TestClient(create_http_app(PredictionServicer(store), store))
    grpc_resp = stub.Predict(request(), timeout=5)
    http_resp = client.post("/predict", json={
        "model_name": "loan_default_xgb", "model_version": "v1", "features": APPLICANT,
    })
    assert http_resp.status_code == 200
    assert http_resp.json()["probabilities"] == pytest.approx(list(grpc_resp.probabilities))

    assert client.post("/predict", json={
        "model_name": "missing", "model_version": "v1", "features": APPLICANT,
    }).status_code == 404
    assert client.get("/health").json()["status"] == "ok"
    assert b"mlservice_predictions_total" in client.get("/metrics").content
