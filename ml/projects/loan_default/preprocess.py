import pandas as pd

def preprocess_data(df):
    binary_cols = [
        'HasMortgage',
        'HasDependents',
        'HasCoSigner'
    ]

    for col in binary_cols:
        df[col] = df[col].map({'Yes': 1, 'No': 0})

    multi_cols = [
        'Education',
        'EmploymentType',
        'MaritalStatus',
        'LoanPurpose'
    ]

    df = pd.get_dummies(
        df,
        columns=multi_cols,
        drop_first=True
    )

    df = df.drop(columns=['LoanID'])

    return df