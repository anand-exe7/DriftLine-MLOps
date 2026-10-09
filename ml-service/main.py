"""
ml-service entry point.

  gRPC  :GRPC_PORT  PredictionService (called by the Go prediction proxy)
  HTTP  :HTTP_PORT  /health /models /metrics /predict (FastAPI, ops + debugging)

Both run in one process and share the same ModelStore, so a model loaded by
one is warm for the other.
"""
import logging
from concurrent import futures
from contextlib import asynccontextmanager

import grpc
import uvicorn

from app.config import load_settings
from app.fetcher import make_minio_fetcher
from app.http_api import create_http_app
from app.model_store import ModelStore
from app.servicer import PredictionServicer
from proto import predict_pb2_grpc

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s")
log = logging.getLogger("ml-service")


def build_grpc_server(servicer: PredictionServicer, port: int, workers: int) -> tuple[grpc.Server, int]:
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=workers))
    predict_pb2_grpc.add_PredictionServiceServicer_to_server(servicer, server)
    bound = server.add_insecure_port(f"[::]:{port}")
    return server, bound


def main():
    settings = load_settings()
    store = ModelStore(settings.model_dir, fetcher=make_minio_fetcher(settings))
    store.preload()
    servicer = PredictionServicer(store)

    grpc_server, port = build_grpc_server(servicer, settings.grpc_port, settings.grpc_workers)

    @asynccontextmanager
    async def lifespan(_app):
        grpc_server.start()
        log.info("gRPC listening on :%d, models loaded: %s", port, store.loaded())
        yield
        log.info("shutting down gRPC (5s grace)")
        grpc_server.stop(grace=5).wait()

    app = create_http_app(servicer, store, lifespan=lifespan)
    uvicorn.run(app, host="0.0.0.0", port=settings.http_port, log_level="info")


if __name__ == "__main__":
    main()
