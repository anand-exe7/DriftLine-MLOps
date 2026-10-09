import pandas as pd
from sklearn.model_selection import train_test_split

TARGET = "Default"
RANDOM_STATE = 42
TEST_SIZE = 0.2

BINARY_COLS = [
    'HasMortgage',
    'HasDependents',
    'HasCoSigner'
]

MULTI_COLS = [
    'Education',
    'EmploymentType',
    'MaritalStatus',
    'LoanPurpose'
]


def preprocess_data(df):
    df = df.copy()

    for col in BINARY_COLS:
        df[col] = df[col].map({'Yes': 1, 'No': 0})

    df = pd.get_dummies(
        df,
        columns=MULTI_COLS,
        drop_first=True
    )

    df = df.drop(columns=['LoanID'])

    # get_dummies yields bool columns; everything downstream (ONNX, baseline,
    # the Go client) works on plain numbers, so make the frame all-float.
    df = df.astype(float)
    df[TARGET] = df[TARGET].astype(int)

    return df


def load_and_split(csv_path="data/Loan_default.csv"):
    """Single source of truth for the train/test split used by every script."""
    df = preprocess_data(pd.read_csv(csv_path))

    X = df.drop(columns=[TARGET])
    y = df[TARGET]

    X_train, X_test, y_train, y_test = train_test_split(
        X,
        y,
        test_size=TEST_SIZE,
        random_state=RANDOM_STATE,
        stratify=y
    )
    return X_train, X_test, y_train, y_test
