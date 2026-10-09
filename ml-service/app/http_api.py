"""
Side-port HTTP API (FastAPI). The hot path is gRPC; this exists for Docker
health checks, Prometheus scraping and manual debugging with curl.
"""
from fastapi import FastAPI, HTTPException, Response
from google.protobuf.json_format import MessageToDict
from prometheus_client import CONTENT_TYPE_LATEST, generate_latest
from pydantic import BaseModel

from proto import predict_pb2

from .servicer import NOT_FOUND, OK, PredictionServicer


class PredictBody(BaseModel):
    model_name: str
    model_version: str
    features: dict[str, float]
    request_id: str = ""


def create_http_app(servicer: PredictionServicer, store, lifespan=None) -> FastAPI:
    app = FastAPI(title="driftline ml-service", lifespan=lifespan)

    @app.get("/health")
    def health():
        return {"status": "ok", "loaded_models": store.loaded()}

    @app.get("/models")
    def models():
        return {"available": store.available(), "loaded": store.loaded()}

    @app.get("/metrics")
    def metrics():
        return Response(generate_latest(), media_type=CONTENT_TYPE_LATEST)

    @app.post("/predict")
    def predict(body: PredictBody):
        # Reuses the gRPC servicer so REST and gRPC can never disagree.
        req = predict_pb2.PredictionRequest(**body.model_dump())
        resp = servicer.Predict(req, None)
        if resp.status_code != OK:
            status = 404 if resp.status_code == NOT_FOUND else 400 if resp.status_code < 13 else 500
            raise HTTPException(status_code=status, detail=resp.error_message)
        return MessageToDict(resp, preserving_proto_field_name=True, always_print_fields_with_no_presence=True)

    return app
