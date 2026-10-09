"""
Retraining loop: train -> register -> (optionally) deploy as a canary.

    python scripts/retrain.py                       # next version, register only
    python scripts/retrain.py --canary 10           # ...and send it 10% of traffic
    python scripts/retrain.py --data path/to.csv    # train on a fresh data export

Versions are numbered automatically (v1, v2, ...) from what the registry holds.
The canary is then watched by the rollback manager; promote it from the
dashboard (Deployments) once it looks healthy.

Run with the training virtualenv (pandas, scikit-learn, xgboost, shap, onnx).
"""
import argparse
import json
import re
import subprocess
import sys
from pathlib import Path

import driftline_api as api

ROOT = Path(__file__).resolve().parents[1]
PROJECT = ROOT / "ml" / "projects" / "loan_default"
MODELS = {"loan_default_lr": "logistic_regression", "loan_default_xgb": "xgboost"}


def next_version(model):
    try:
        versions = api.get(f"/models/{model}/versions")
    except api.ApiError as err:
        if err.status == 404:
            return "v1"
        raise
    nums = [int(m.group(1)) for v in versions if (m := re.fullmatch(r"v(\d+)", v["version"]))]
    return f"v{max(nums, default=0) + 1}"


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--canary", type=int, default=0, help="deploy each new version as a canary with this %% of traffic")
    ap.add_argument("--data", help="CSV to train on (default: the project dataset)")
    ap.add_argument("--models", nargs="+", default=list(MODELS), choices=list(MODELS))
    args = ap.parse_args()

    # One version number for the run, so LR and XGB trained together share it.
    version = max((next_version(m) for m in args.models), key=lambda v: int(v[1:]))
    print(f"==> training {version}")

    cmd = [sys.executable, "train.py", "--version", version, "--no-publish"]
    if args.data:
        cmd += ["--data", str(Path(args.data).resolve())]
    subprocess.run(cmd, cwd=PROJECT, check=True)

    metrics = json.loads((PROJECT / "artifacts" / "evaluation_metrics.json").read_text())
    shap = json.loads((PROJECT / "artifacts" / "shap_importance.json").read_text())
    baseline = (PROJECT / "artifacts" / "baseline_stats.json").read_bytes()

    for model in args.models:
        key = MODELS[model]
        bundle = PROJECT / "onnx" / model / version
        model_metrics = dict(metrics[key], shap_importance=shap[key])

        try:
            api.post("/models", {"name": model, "description": f"loan default ({key})"})
        except api.ApiError as err:
            if err.status != 409:
                raise

        print(f"==> registering {model}/{version}")
        api.post_multipart(
            f"/models/{model}/versions",
            {"version": version},
            {
                "model": ("model.onnx", (bundle / "model.onnx").read_bytes()),
                "feature_schema": ("feature_schema.json", (bundle / "feature_schema.json").read_bytes()),
                "metrics": ("metrics.json", json.dumps(model_metrics).encode()),
                "baseline_stats": ("baseline_stats.json", baseline),
            },
        )
        print(f"    ROC-AUC {model_metrics['roc_auc']:.4f}  recall {model_metrics['recall']:.4f}  F1 {model_metrics['f1']:.4f}")

        if args.canary:
            try:
                d = api.post("/deployments", {"model": model, "version": version, "traffic_percent": args.canary})
                print(f"    deployed as {d['role']} with {d['traffic_percent']}% of traffic")
            except api.ApiError as err:
                print(f"    not deployed: {err}")

    print("done")


if __name__ == "__main__":
    main()
