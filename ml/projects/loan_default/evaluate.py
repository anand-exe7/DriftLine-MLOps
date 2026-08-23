import os
import json
import joblib
import pandas as pd

from sklearn.model_selection import train_test_split
from sklearn.metrics import (
    accuracy_score,
    precision_score,
    recall_score,
    f1_score,
    roc_auc_score,
    confusion_matrix
)

from preprocess import preprocess_data


df = pd.read_csv("data/Loan_default.csv")

df = preprocess_data(df)

X = df.drop(columns=["Default"])
y = df["Default"]



X_train, X_test, y_train, y_test = train_test_split(
    X,
    y,
    test_size=0.2,
    random_state=42,
    stratify=y
)


lr = joblib.load("weights/lr_model.pkl")
xgb = joblib.load("weights/xgb_model.pkl")


models = {
    "logistic_regression": lr,
    "xgboost": xgb
}


results = {}

for name, model in models.items():

    predictions = model.predict(X_test)

    probabilities = model.predict_proba(X_test)[:, 1]

    results[name] = {
        "accuracy": float(
            accuracy_score(y_test, predictions)
        ),

        "precision": float(
            precision_score(
                y_test,
                predictions,
                zero_division=0
            )
        ),

        "recall": float(
            recall_score(
                y_test,
                predictions,
                zero_division=0
            )
        ),

        "f1": float(
            f1_score(
                y_test,
                predictions,
                zero_division=0
            )
        ),

        "roc_auc": float(
            roc_auc_score(
                y_test,
                probabilities
            )
        ),

        "confusion_matrix": (
            confusion_matrix(
                y_test,
                predictions
            ).tolist()
        )
    }


os.makedirs("artifacts", exist_ok=True)

with open(
    "artifacts/evaluation_metrics.json",
    "w"
) as file:

    json.dump(
        results,
        file,
        indent=2
    )

for name, metrics in results.items():

    print(f"\n{name}")

    print(
        "Accuracy :",
        metrics["accuracy"]
    )

    print(
        "Precision:",
        metrics["precision"]
    )

    print(
        "Recall   :",
        metrics["recall"]
    )

    print(
        "F1       :",
        metrics["f1"]
    )

    print(
        "ROC-AUC  :",
        metrics["roc_auc"]
    )

    print(
        "Confusion Matrix:"
    )

    print(
        metrics["confusion_matrix"]
    )

print(
    "\nSaved → artifacts/evaluation_metrics.json"
)