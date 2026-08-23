import pandas as pd

from sklearn.model_selection import train_test_split
from sklearn.linear_model import LogisticRegression
from xgboost import XGBClassifier

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

lr = LogisticRegression(max_iter=5000)
lr.fit(X_train, y_train)

xgb = XGBClassifier(
    eval_metric="logloss",
    random_state=42
)
xgb.fit(X_train, y_train)