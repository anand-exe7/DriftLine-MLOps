import os
import json
import pandas as pd


def generate_baseline_stats(X_train):

    baseline = {
        "dataset": {
            "feature_count": int(X_train.shape[1]),
            "sample_count": int(X_train.shape[0])
        },
        "features": {}
    }

    for column in X_train.columns:

        series = X_train[column]

        # Convert boolean columns (True/False) to 1/0
        if series.dtype == "bool":
            series = series.astype(int)

        # Convert everything possible to numeric
        series = pd.to_numeric(
            series,
            errors="coerce"
        )

        values = series.dropna()

        # Skip empty columns
        if len(values) == 0:
            continue


        # Deciles
        deciles = {
            f"p{i*10}": float(values.quantile(i / 10))
            for i in range(1, 10)
        }


        baseline["features"][column] = {

            "mean": float(
                values.mean()
            ),

            "std": float(
                values.std()
            ),

            "min": float(
                values.min()
            ),

            "max": float(
                values.max()
            ),

            "missing_rate": float(
                series.isna().mean()
            ),

            "deciles": deciles
        }


    return baseline



def save_baseline_stats(
        baseline,
        output_path
):

    os.makedirs(
        os.path.dirname(output_path),
        exist_ok=True
    )

    with open(
        output_path,
        "w"
    ) as file:

        json.dump(
            baseline,
            file,
            indent=4
        )



if __name__ == "__main__":


    print("Loading Dataset")

    df = pd.read_csv(
        "data/Loan_default.csv"
    )


    print("Preprocessing")


    # Import your preprocessing function
    from preprocess import preprocess_data


    df = preprocess_data(df)


    # Separate features
    X = df.drop(
        columns=["Default"]
    )


    print(
        "Generating baseline..."
    )


    baseline = generate_baseline_stats(
        X
    )


    save_baseline_stats(
        baseline,
        "artifacts/baseline_stats.json"
    )


    print(
        "Baseline generated successfully"
    )

    print(
        "Saved at artifacts/baseline_stats.json"
    )