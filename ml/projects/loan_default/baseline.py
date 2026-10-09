import os
import json
import numpy as np
import pandas as pd

from preprocess import load_and_split


def _bins(values, is_binary, deciles):
    """
    Reference distribution for PSI.

    Continuous features: bins are cut at the (de-duplicated) decile values, with
    open-ended outer bins, i.e. (-inf, p10], (p10, p20], ..., (p90, +inf).
    The Go drift engine must bucket live values with the same edges and the
    same right-closed rule, then compare its proportions against these.

    Binary features: two buckets, value 0 and value 1.
    """
    if is_binary:
        return {
            "edges": [0.5],
            "proportions": [float((values == 0).mean()), float((values == 1).mean())]
        }

    edges = sorted(set(deciles.values()))
    # side="left" => value v lands in bin i where edges[i-1] < v <= edges[i]
    idx = np.searchsorted(np.asarray(edges), values.to_numpy(), side="left")
    counts = np.bincount(idx, minlength=len(edges) + 1)
    return {
        "edges": [float(e) for e in edges],
        "proportions": [float(c) / len(values) for c in counts]
    }


def generate_baseline_stats(X_train):

    baseline = {
        "dataset": {
            "feature_count": int(X_train.shape[1]),
            "sample_count": int(X_train.shape[0]),
            "source": "training split only"
        },
        "feature_order": list(X_train.columns),
        "features": {}
    }

    for column in X_train.columns:

        series = pd.to_numeric(X_train[column], errors="coerce")
        values = series.dropna()

        if len(values) == 0:
            continue

        is_binary = set(values.unique()).issubset({0.0, 1.0})

        deciles = {
            f"p{i*10}": float(values.quantile(i / 10))
            for i in range(1, 10)
        }

        baseline["features"][column] = {
            "type": "binary" if is_binary else "continuous",
            "mean": float(values.mean()),
            "std": float(values.std()),
            "min": float(values.min()),
            "max": float(values.max()),
            "missing_rate": float(series.isna().mean()),
            "deciles": deciles,
            "bins": _bins(values, is_binary, deciles)
        }

    return baseline


def save_baseline_stats(baseline, output_path):
    os.makedirs(os.path.dirname(output_path), exist_ok=True)
    with open(output_path, "w") as file:
        json.dump(baseline, file, indent=4)


if __name__ == "__main__":
    print("Loading + splitting dataset")
    X_train, _, _, _ = load_and_split()

    print("Generating baseline from training split...")
    save_baseline_stats(generate_baseline_stats(X_train), "artifacts/baseline_stats.json")
    print("Saved at artifacts/baseline_stats.json")
