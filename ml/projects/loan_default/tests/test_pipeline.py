import sys
from pathlib import Path

import numpy as np
import pandas as pd
import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from baseline import generate_baseline_stats  # noqa: E402
from convert_to_onnx import export_LR_to_onnx, export_XG_to_onnx, verify_parity  # noqa: E402
from preprocess import TARGET, preprocess_data  # noqa: E402
from train import train_LR_model, train_xgb_model  # noqa: E402

DATA = Path(__file__).resolve().parents[1] / "data" / "Loan_default.csv"


@pytest.fixture(scope="module")
def sample():
    df = preprocess_data(pd.read_csv(DATA, nrows=4000))
    X = df.drop(columns=[TARGET])
    return X, df[TARGET]


def test_preprocess_is_all_numeric(sample):
    X, y = sample
    assert X.shape[1] == 24
    assert all(dtype == np.float64 for dtype in X.dtypes)
    assert set(y.unique()) <= {0, 1}
    assert "LoanID" not in X.columns


def test_baseline_bins_are_distributions(sample):
    X, _ = sample
    baseline = generate_baseline_stats(X)
    assert baseline["feature_order"] == list(X.columns)
    for name, stats in baseline["features"].items():
        props = stats["bins"]["proportions"]
        assert len(props) == len(stats["bins"]["edges"]) + 1, name
        assert sum(props) == pytest.approx(1.0), name


@pytest.mark.parametrize("trainer,exporter", [
    (train_LR_model, export_LR_to_onnx),
    (train_xgb_model, export_XG_to_onnx),
])
def test_onnx_matches_original(sample, trainer, exporter):
    X, y = sample
    X32 = X.to_numpy(np.float32)
    model = trainer(X32, y.to_numpy())
    onnx_bytes = exporter(model, X32.shape[1]).SerializeToString()
    parity = verify_parity("test", model, onnx_bytes, X32)
    assert parity["label_agreement"] == 1.0
