from prometheus_client import Counter, Histogram

PREDICTIONS = Counter(
    "mlservice_predictions_total",
    "Predict calls by model, version and outcome",
    ["model", "version", "status"],
)

LATENCY = Histogram(
    "mlservice_inference_seconds",
    "Time spent vectorizing + running ONNX inference",
    ["model", "version"],
    buckets=(0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25),
)
