import logging
import time

import grpc

from proto import predict_pb2, predict_pb2_grpc

from .metrics import LATENCY, PREDICTIONS
from .model_store import InvalidFeatures, ModelNotFound, ModelStore

log = logging.getLogger(__name__)

# Same numbering as google.rpc.Code / grpc.StatusCode so the Go side can map them directly.
OK = grpc.StatusCode.OK.value[0]                              # 0
INVALID_ARGUMENT = grpc.StatusCode.INVALID_ARGUMENT.value[0]  # 3
NOT_FOUND = grpc.StatusCode.NOT_FOUND.value[0]                # 5
INTERNAL = grpc.StatusCode.INTERNAL.value[0]                  # 13


class PredictionServicer(predict_pb2_grpc.PredictionServiceServicer):
    """
    Application-level errors (bad features, unknown model) are reported inside
    PredictionResponse.status_code/error_message with a transport-level OK.
    That keeps "the RPC worked but the input was wrong" distinct from
    "the RPC itself failed" (network, deadline), which surface as gRPC errors.
    """

    def __init__(self, store: ModelStore):
        self._store = store

    def Predict(self, request, context):
        name, version = request.model_name, request.model_version
        resp = predict_pb2.PredictionResponse(request_id=request.request_id, model_version_used=version)

        if not name or not version:
            return self._fail(resp, name, version, INVALID_ARGUMENT, "model_name and model_version are required")

        start = time.perf_counter()
        try:
            model = self._store.get(name, version)
            x = model.vectorize(request.features)
            probs = model.predict_proba(x)[0]
        except ModelNotFound as err:
            # don't label metrics with unknown names: arbitrary client input would explode cardinality
            return self._fail(resp, "", "", NOT_FOUND, str(err))
        except InvalidFeatures as err:
            return self._fail(resp, name, version, INVALID_ARGUMENT, str(err))
        except Exception as err:  # never let one bad request kill the worker thread
            log.exception("inference failed for %s/%s", name, version)
            return self._fail(resp, name, version, INTERNAL, f"inference error: {err}")
        elapsed = time.perf_counter() - start

        resp.prediction = int(probs.argmax())
        resp.probabilities.extend(float(p) for p in probs)
        resp.latency_ms = int(round(elapsed * 1000))
        resp.status_code = OK

        PREDICTIONS.labels(name, version, "ok").inc()
        LATENCY.labels(name, version).observe(elapsed)
        return resp

    def HealthCheck(self, request, context):
        return predict_pb2.HealthResponse(status="SERVING", loaded_models=self._store.loaded())

    @staticmethod
    def _fail(resp, name, version, code, message):
        PREDICTIONS.labels(name or "-", version or "-", "error").inc()
        resp.status_code = code
        resp.error_message = message
        return resp
