import os
import json
import joblib
import numpy as np

from sklearn.metrics import (
    accuracy_score,
    precision_score,
    recall_score,
    f1_score,
    roc_auc_score,
    confusion_matrix
)

from preprocess import load_and_split


def evaluate_model(model, X_test, y_test):
    predictions = model.predict(X_test)
    probabilities = model.predict_proba(X_test)[:, 1]

    return {
        "accuracy": float(accuracy_score(y_test, predictions)),
        "precision": float(precision_score(y_test, predictions, zero_division=0)),
        "recall": float(recall_score(y_test, predictions, zero_division=0)),
        "f1": float(f1_score(y_test, predictions, zero_division=0)),
        "roc_auc": float(roc_auc_score(y_test, probabilities)),
        "confusion_matrix": confusion_matrix(y_test, predictions).tolist()
    }


def evaluate_models(models, X_test, y_test, output_path="artifacts/evaluation_metrics.json"):
    """models: {name: fitted estimator}. X_test must be the float32 matrix the models were trained on."""
    results = {name: evaluate_model(model, X_test, y_test) for name, model in models.items()}

    os.makedirs(os.path.dirname(output_path), exist_ok=True)
    with open(output_path, "w") as file:
        json.dump(results, file, indent=2)

    for name, metrics in results.items():
        print(f"\n     {name}")
        print("     Accuracy :", round(metrics["accuracy"], 4))
        print("     Precision:", round(metrics["precision"], 4))
        print("     Recall   :", round(metrics["recall"], 4))
        print("     F1       :", round(metrics["f1"], 4))
        print("     ROC-AUC  :", round(metrics["roc_auc"], 4))
        print("     Confusion Matrix:", metrics["confusion_matrix"])

    print(f"\n     Saved -> {output_path}")
    return results


if __name__ == "__main__":
    # Re-evaluate already-trained checkpoints without retraining.
    _, X_test, _, y_test = load_and_split()

    models = {
        "logistic_regression": joblib.load("weights/lr_model.pkl"),
        "xgboost": joblib.load("weights/xgb_model.pkl"),
    }
    evaluate_models(models, X_test.to_numpy(np.float32), y_test.to_numpy())
