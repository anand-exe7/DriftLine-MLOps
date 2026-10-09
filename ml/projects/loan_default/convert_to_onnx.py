import os
import json
import numpy as np
import onnxmltools
import onnxruntime as ort
from skl2onnx import convert_sklearn

from skl2onnx.common.data_types import FloatTensorType as SklearnFloatTensorType
from onnxmltools.convert.common.data_types import FloatTensorType as XGBFloatTensorType

INPUT_NAME = "float_input"
PARITY_ATOL = 1e-4


def export_LR_to_onnx(lr_pipeline, n_features):
    initial_type = [(INPUT_NAME, SklearnFloatTensorType([None, n_features]))]

    # zipmap=False -> probabilities come out as a plain [N, 2] float tensor
    # instead of a list of {class: prob} dicts, which is what the server expects.
    return convert_sklearn(
        lr_pipeline,
        initial_types=initial_type,
        target_opset=17,
        options={id(lr_pipeline.named_steps["lr"]): {"zipmap": False}}
    )


def export_XG_to_onnx(xgb, n_features):
    initial_types = [(INPUT_NAME, XGBFloatTensorType([None, n_features]))]

    return onnxmltools.convert_xgboost(
        xgb,
        initial_types=initial_types,
        target_opset=15
    )


def onnx_predict_proba(onnx_bytes, X):
    session = ort.InferenceSession(onnx_bytes, providers=["CPUExecutionProvider"])
    outputs = session.run(None, {INPUT_NAME: X})
    # outputs = [label, probabilities]
    return np.asarray(outputs[1])


def verify_parity(name, model, onnx_bytes, X_test):
    """ONNX Runtime must reproduce the original model's probabilities."""
    expected = model.predict_proba(X_test)
    actual = onnx_predict_proba(onnx_bytes, X_test)

    max_diff = float(np.max(np.abs(expected - actual)))
    label_agreement = float(np.mean(expected.argmax(axis=1) == actual.argmax(axis=1)))

    print(f"     [{name}] max |p_sklearn - p_onnx| = {max_diff:.2e}, label agreement = {label_agreement:.5f}")
    if max_diff > PARITY_ATOL:
        raise AssertionError(f"{name}: ONNX output diverges from original model (max diff {max_diff})")

    return {"max_abs_prob_diff": max_diff, "label_agreement": label_agreement}


def write_model_bundle(out_dir, onnx_bytes, schema):
    """A bundle is everything ml-service needs: model.onnx + feature_schema.json."""
    os.makedirs(out_dir, exist_ok=True)
    with open(os.path.join(out_dir, "model.onnx"), "wb") as f:
        f.write(onnx_bytes)
    with open(os.path.join(out_dir, "feature_schema.json"), "w") as f:
        json.dump(schema, f, indent=2)


def export_onnx_models(lr_pipeline, xgb, X_test, feature_names, metrics, version, out_root="onnx"):
    """
    Exports both models, verifies parity on X_test, writes bundles to
    {out_root}/{model_name}/{version}/ and returns {model_name: bundle_dir}.
    """
    n_features = len(feature_names)
    specs = {
        "loan_default_lr": (lr_pipeline, export_LR_to_onnx(lr_pipeline, n_features), "sklearn", "logistic_regression"),
        "loan_default_xgb": (xgb, export_XG_to_onnx(xgb, n_features), "xgboost", "xgboost"),
    }

    bundles = {}
    for model_name, (model, onnx_model, framework, metrics_key) in specs.items():
        onnx_bytes = onnx_model.SerializeToString()
        parity = verify_parity(model_name, model, onnx_bytes, X_test)

        schema = {
            "model_name": model_name,
            "version": version,
            "framework": framework,
            "input_name": INPUT_NAME,
            "features": list(feature_names),
            "classes": [0, 1],
            "metrics": metrics[metrics_key],
            "onnx_parity": parity,
        }
        out_dir = os.path.join(out_root, model_name, version)
        write_model_bundle(out_dir, onnx_bytes, schema)
        bundles[model_name] = out_dir
        print(f"     Wrote {out_dir}/model.onnx ({len(onnx_bytes) / 1024:.0f} KB)")

    return bundles
