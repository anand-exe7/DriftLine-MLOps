"""
ModelStore: lazily loads ONNX bundles and caches one InferenceSession per
(model_name, version). Loading a session is expensive (parse graph, allocate),
running it is cheap and thread-safe, so we load once and share across requests.

On-disk layout (identical to the MinIO object layout used by the Go registry):

    {MODEL_DIR}/{model_name}/{version}/model.onnx
    {MODEL_DIR}/{model_name}/{version}/feature_schema.json
"""
import json
import logging
import re
import threading
from dataclasses import dataclass
from pathlib import Path
from typing import Mapping

import numpy as np
import onnxruntime as ort

log = logging.getLogger(__name__)

# Names come straight from the network; restrict them so "../../etc" can't escape MODEL_DIR.
_SAFE_SEGMENT = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$")

MODEL_FILE = "model.onnx"
SCHEMA_FILE = "feature_schema.json"


class ModelNotFound(Exception):
    pass


class InvalidFeatures(Exception):
    pass


@dataclass
class LoadedModel:
    name: str
    version: str
    session: ort.InferenceSession
    input_name: str
    features: list[str]
    prob_output: str

    def vectorize(self, features: Mapping[str, float]) -> np.ndarray:
        """Map {feature: value} -> [1, n_features] float32 in training column order."""
        expected = set(self.features)
        got = set(features.keys())
        missing = sorted(expected - got)
        unknown = sorted(got - expected)
        if missing or unknown:
            parts = []
            if missing:
                parts.append(f"missing features: {missing}")
            if unknown:
                parts.append(f"unknown features: {unknown}")
            raise InvalidFeatures("; ".join(parts))

        row = np.fromiter((features[f] for f in self.features), dtype=np.float32, count=len(self.features))
        if not np.all(np.isfinite(row)):
            raise InvalidFeatures("feature values must be finite numbers")
        return row.reshape(1, -1)

    def predict_proba(self, x: np.ndarray) -> np.ndarray:
        (probs,) = self.session.run([self.prob_output], {self.input_name: x})
        return np.asarray(probs)


class ModelStore:
    def __init__(self, model_dir: Path, fetcher=None):
        self._model_dir = Path(model_dir)
        self._fetcher = fetcher  # optional: callable(name, version, dest_dir) -> bool
        self._models: dict[tuple[str, str], LoadedModel] = {}
        self._lock = threading.Lock()

    def loaded(self) -> list[str]:
        with self._lock:
            return sorted(f"{n}/{v}" for n, v in self._models)

    def available(self) -> list[str]:
        """Bundles present on local disk (loaded or not)."""
        return sorted(
            f"{p.parent.parent.name}/{p.parent.name}"
            for p in self._model_dir.glob(f"*/*/{MODEL_FILE}")
        )

    def get(self, name: str, version: str) -> LoadedModel:
        key = (name, version)
        # Fast path without the lock: dict reads are atomic in CPython.
        model = self._models.get(key)
        if model is not None:
            return model

        # Slow path: one loader at a time, re-check after acquiring the lock so
        # concurrent first requests for the same model don't load it twice.
        with self._lock:
            model = self._models.get(key)
            if model is None:
                model = self._load(name, version)
                self._models[key] = model
            return model

    def preload(self) -> None:
        for entry in self.available():
            name, version = entry.split("/")
            try:
                self.get(name, version)
            except Exception:  # a bad bundle shouldn't stop the server booting
                log.exception("failed to preload %s", entry)

    def _load(self, name: str, version: str) -> LoadedModel:
        if not (_SAFE_SEGMENT.match(name) and _SAFE_SEGMENT.match(version)):
            raise ModelNotFound(f"invalid model name/version: {name!r}/{version!r}")

        bundle = self._model_dir / name / version
        if not (bundle / MODEL_FILE).exists() and self._fetcher is not None:
            log.info("model %s/%s not on disk, fetching from object storage", name, version)
            self._fetcher(name, version, bundle)

        model_path, schema_path = bundle / MODEL_FILE, bundle / SCHEMA_FILE
        if not model_path.exists() or not schema_path.exists():
            raise ModelNotFound(f"model {name}/{version} not found")

        schema = json.loads(schema_path.read_text())
        opts = ort.SessionOptions()
        opts.intra_op_num_threads = 1  # parallelism comes from the gRPC thread pool instead
        session = ort.InferenceSession(str(model_path), opts, providers=["CPUExecutionProvider"])

        outputs = [o.name for o in session.get_outputs()]
        prob_output = "probabilities" if "probabilities" in outputs else outputs[-1]

        log.info("loaded model %s/%s (%d features)", name, version, len(schema["features"]))
        return LoadedModel(
            name=name,
            version=version,
            session=session,
            input_name=schema.get("input_name") or session.get_inputs()[0].name,
            features=list(schema["features"]),
            prob_output=prob_output,
        )
