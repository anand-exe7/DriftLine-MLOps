"""
End-to-end offline pipeline for the loan-default models:

    load + split -> train LR & XGB -> evaluate -> baseline stats -> SHAP
    -> ONNX export + parity check -> publish bundles to ml-service/models

Run from this directory:  python train.py [--version v1] [--no-publish]
"""
import argparse
import os
import shutil
from pathlib import Path

import joblib
import numpy as np
from sklearn.linear_model import LogisticRegression
from sklearn.pipeline import Pipeline
from sklearn.preprocessing import StandardScaler
from xgboost import XGBClassifier

from baseline import generate_baseline_stats, save_baseline_stats
from convert_to_onnx import export_onnx_models
from evaluate import evaluate_models
from explain import save_shap_importance, shap_importance
from preprocess import RANDOM_STATE, load_and_split

SERVING_MODEL_DIR = Path(__file__).resolve().parents[3] / "ml-service" / "models"
SHAP_SAMPLE = 2000


def train_LR_model(X_train, y_train):
    # Scaling matters here: raw Income/LoanAmount are ~1e5 while the dummies are 0/1,
    # so unscaled LR converges slowly and effectively ignores small-range features.
    # class_weight="balanced" counters the ~88/12 class imbalance (otherwise the
    # model predicts "no default" for almost everyone and recall is ~3%).
    lr = Pipeline([
        ("scaler", StandardScaler()),
        ("lr", LogisticRegression(max_iter=5000, class_weight="balanced")),
    ])
    lr.fit(X_train, y_train)
    return lr


def train_xgb_model(X_train, y_train):
    neg, pos = np.bincount(y_train)
    xgb = XGBClassifier(
        n_estimators=300,
        max_depth=5,
        learning_rate=0.1,
        subsample=0.8,
        colsample_bytree=0.8,
        scale_pos_weight=neg / pos,  # same idea as class_weight="balanced"
        eval_metric="logloss",
        random_state=RANDOM_STATE,
        n_jobs=-1,
    )
    xgb.fit(X_train, y_train)
    return xgb


def save_checkpoints(lr, xgb):
    os.makedirs("weights", exist_ok=True)
    joblib.dump(lr, "weights/lr_model.pkl")
    joblib.dump(xgb, "weights/xgb_model.pkl")


def publish(bundles):
    """Copy {name}/{version} bundles into ml-service/models so the server can load them."""
    for model_name, bundle_dir in bundles.items():
        version = Path(bundle_dir).name
        target = SERVING_MODEL_DIR / model_name / version
        if target.exists():
            shutil.rmtree(target)
        shutil.copytree(bundle_dir, target)
        print(f"     Published -> {target}")


def train(version="v1", do_publish=True):
    print("     Loading + splitting dataset")
    X_train_df, X_test_df, y_train_s, y_test_s = load_and_split()
    feature_names = list(X_train_df.columns)

    # Both models train on the exact float32 matrix ONNX will receive at serving
    # time, so parity checks compare like with like.
    X_train = X_train_df.to_numpy(np.float32)
    X_test = X_test_df.to_numpy(np.float32)
    y_train = y_train_s.to_numpy()
    y_test = y_test_s.to_numpy()
    print(f"     train={X_train.shape}, test={X_test.shape}, positive rate={y_train.mean():.3f}")

    print("     Training Logistic Regression")
    lr = train_LR_model(X_train, y_train)

    print("     Training XGBoost")
    xgb = train_xgb_model(X_train, y_train)

    print("     Saving .pkl checkpoints to weights/")
    save_checkpoints(lr, xgb)

    print("     Evaluating")
    metrics = evaluate_models({"logistic_regression": lr, "xgboost": xgb}, X_test, y_test)

    print("     Generating baseline stats (training split)")
    save_baseline_stats(generate_baseline_stats(X_train_df), "artifacts/baseline_stats.json")

    print("     Computing SHAP importance")
    rng = np.random.default_rng(RANDOM_STATE)
    background = X_train[rng.choice(len(X_train), SHAP_SAMPLE, replace=False)]
    explain = X_test[rng.choice(len(X_test), SHAP_SAMPLE, replace=False)]
    save_shap_importance(shap_importance(lr, xgb, background, explain, feature_names))

    print("     Exporting to ONNX + verifying parity")
    bundles = export_onnx_models(lr, xgb, X_test, feature_names, metrics, version)

    if do_publish:
        publish(bundles)

    print("     Done")


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--version", default="v1")
    parser.add_argument("--no-publish", action="store_true")
    args = parser.parse_args()
    train(args.version, not args.no_publish)
