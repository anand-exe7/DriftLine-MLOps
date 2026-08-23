import json
import os

import numpy as np
import pandas as pd


def generate_baseline_stats(X_train):
    """
    Generate baseline statistics from training features.

    These statistics will later be used by Driftline's
    drift engine to compare production data against
    the original training distribution.
    """

    baseline = {    
        "dataset": {
            "feature_count": int(X_train.shape[1]),
            "sample_count": int(X_train.shape[0])
        },
        "features": {}
    }

    for column in X_train.columns:

        series = pd.to_numeric(
            X_train[column],
            errors="coerce"
        )

        values = series.dropna()

        if len(values) == 0:
            continue

        deciles = {
            f"p{i * 10}": float(values.quantile(i / 10))
            for i in range(1, 10)
        }

        missing_rate = float(series.isna().mean())

        baseline["features"][column] = {
            "mean": float(values.mean()),
            "std": float(values.std()),
            "min": float(values.min()),
            "max": float(values.max()),
            "missing_rate": missing_rate,
            "deciles": deciles
        }

    return baseline


def save_baseline_stats(baseline, output_path):
    """Save baseline statistics as JSON."""

    os.makedirs(
        os.path.dirname(output_path),
        exist_ok=True
    )

    with open(output_path, "w") as file:
        json.dump(
            baseline,
            file,
            indent=2
        )


if __name__ == "__main__":

    df = pd.read_csv("data/Loan_default.csv")

    from preprocess import preprocess_data

    df = preprocess_data(df)

    X = df.drop(columns=["Default"])

    # Generate baseline
    baseline = generate_baseline_stats(X)

    # Save artifact
    save_baseline_stats(
        baseline,
        "artifacts/baseline_stats.json"
    )

    print("Baseline statistics generated successfully.")