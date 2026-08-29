import pandas as pd

from sklearn.model_selection import train_test_split
from sklearn.linear_model import LogisticRegression
from xgboost import XGBClassifier

from preprocess import preprocess_data

print("Loading Dataset")
df = pd.read_csv("data/Loan_default.csv")

print("preprocesing")
df = preprocess_data(df)

X = df.drop(columns=["Default"])
y = df["Default"]

print("Test Train Split")
X_train_processed, X_test, y_train, y_test = train_test_split(
    X,
    y,
    test_size=0.2,
    random_state=42,
    stratify=y
)
import numpy as np

print("Shape:", X_train_processed.shape)
print("Min:", np.min(X_train_processed))
print("Max:", np.max(X_train_processed))
print("Mean:", np.mean(X_train_processed))
print("Std:", np.std(X_train_processed))

print("training Logistic Regression")
lr = LogisticRegression(max_iter=5000)
lr.fit(X_train_processed, y_train)

print("training XG Boost")
xgb = XGBClassifier(
    eval_metric="logloss",
    random_state=42
)
xgb.fit(X_train_processed, y_train)

from pathlib import Path

folder_path = Path("weights")

folder_path.mkdir(parents=True, exist_ok=True)

import joblib

joblib.dump(lr, 'weights/lr_model.pkl')
joblib.dump(xgb, 'weights/xgb_model.pkl')