import os
import json
import numpy as np
import shap


def _importance(shap_values, feature_names):
    """Global importance = mean |SHAP| per feature, sorted descending."""
    mean_abs = np.abs(shap_values).mean(axis=0)
    ranked = sorted(zip(feature_names, mean_abs), key=lambda kv: kv[1], reverse=True)
    return [{"feature": name, "mean_abs_shap": float(value)} for name, value in ranked]


def shap_importance(lr_pipeline, xgb, X_background, X_explain, feature_names):
    """
    lr_pipeline: Pipeline(StandardScaler -> LogisticRegression)
    X_background: sample of training data (float32 ndarray) used as the reference
    X_explain:    sample of held-out data to explain
    """
    scaler = lr_pipeline.named_steps["scaler"]
    lr = lr_pipeline.named_steps["lr"]

    # LR is linear in the *scaled* space, so explain it there.
    masker = shap.maskers.Independent(scaler.transform(X_background), max_samples=len(X_background))
    lr_explainer = shap.LinearExplainer(lr, masker)
    lr_values = lr_explainer.shap_values(scaler.transform(X_explain))

    xgb_explainer = shap.TreeExplainer(xgb)
    xgb_values = xgb_explainer.shap_values(X_explain)

    return {
        "method": "mean(|SHAP|) over held-out sample",
        "sample_size": int(X_explain.shape[0]),
        "logistic_regression": _importance(lr_values, feature_names),
        "xgboost": _importance(xgb_values, feature_names),
    }


def save_shap_importance(result, output_path="artifacts/shap_importance.json"):
    os.makedirs(os.path.dirname(output_path), exist_ok=True)
    with open(output_path, "w") as file:
        json.dump(result, file, indent=2)

    for model in ("logistic_regression", "xgboost"):
        top = ", ".join(r["feature"] for r in result[model][:5])
        print(f"     {model} top-5: {top}")
    print(f"     Saved -> {output_path}")
