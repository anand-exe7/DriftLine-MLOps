import pandas as pd

from sklearn.model_selection import train_test_split
from sklearn.linear_model import LogisticRegression
from xgboost import XGBClassifier

from preprocess import preprocess_data
from convert_to_onnx import export_onnx_models

def load_data():
    try:
        df = pd.read_csv("data/Loan_default.csv")
        df = preprocess_data(df)

        X = df.drop(columns=["Default"])
        y = df["Default"]

    except FileNotFoundError:
        print("Dataset not found. Please ensure 'data/Loan_default.csv' exists.")
        return None, None
    except Exception as e:
        print(f"Error loading data: {e}")
        return None, None

    return X,y

def split_data(X,y):
    X_train_processed, X_test, y_train, y_test = train_test_split(
        X,
        y,
        test_size=0.2,
        random_state=42,
        stratify=y
    )
    return X_train_processed, X_test, y_train, y_test

import numpy as np

def stats(X_train_processed):
    print("     ----------STATS----------")
    print("     Shape:", X_train_processed.shape)
    print("     Min:", np.min(X_train_processed))
    print("     Max:", np.max(X_train_processed))
    print("     Mean:", np.mean(X_train_processed))
    print("     Std:", np.std(X_train_processed))
    print("     -------------------------")

    return 
def train_LR_model(X_train_processed, y_train):
    lr = LogisticRegression(max_iter=5000)
    lr.fit(X_train_processed, y_train)

    return lr

def train_xgb_model(X_train_processed, y_train):
    xgb = XGBClassifier(
        eval_metric="logloss",
        random_state=42
    )
    xgb.fit(X_train_processed.to_numpy(), y_train)
    
    return xgb
def export_models(lr,xgb):
    from pathlib import Path

    folder_path = Path("weights")

    folder_path.mkdir(parents=True, exist_ok=True)

    import joblib

    joblib.dump(lr, 'weights/lr_model.pkl')
    joblib.dump(xgb, 'weights/xgb_model.pkl')

def train():

    print("     Loading Dataset")
    X,y = load_data()

    if X is None or y is None:
        print("Training stopped because dataset could not be loaded.")
        return
    
    print("     Splitting Dataset into Test and Train")
    X_train_processed, X_test, y_train, y_test = split_data(X,y)
    stats(X_train_processed)


    print("     Training Linear Regression Model")
    lr = train_LR_model(X_train_processed, y_train)


    print("     Training XBOOST Model")
    xgb = train_xgb_model(X_train_processed, y_train)


    print("     Exporting .pkl Files to /weights")
    export_models(lr,xgb)


    print("     Converting and Exporting .pkl Files to .onnx")
    export_onnx_models(lr,xgb,X_train_processed)

    
    print("     Models Saved")

if __name__ == "__main__":

    train()
