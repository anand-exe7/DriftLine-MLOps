import onnxmltools
from skl2onnx import convert_sklearn

from skl2onnx.common.data_types import FloatTensorType as SklearnFloatTensorType
from onnxmltools.convert.common.data_types import FloatTensorType as XGBFloatTensorType

def export_LR_to_onnx(lr,X_train):
    
    try:
       
        initial_type = [
        ("float_input", SklearnFloatTensorType([None, X_train.shape[1]]))
    ]

        onnx_model = convert_sklearn(
            lr,
            initial_types=initial_type,
            target_opset=17
        )

        with open("weights/lr_model.onnx", "wb") as f:
            f.write(onnx_model.SerializeToString())

    except Exception as e:
        print(f"Conversion failed: {e}")

    return 

def export_XG_to_onnx(xgb, X_train):

    initial_types = [
        ("float_input", XGBFloatTensorType([None, X_train.shape[1]]))
    ]

    onnx_model = onnxmltools.convert_xgboost(
        xgb,
        initial_types=initial_types,
        target_opset=15
    )

    with open("weights/xgboost_model.onnx", "wb") as f:
        f.write(onnx_model.SerializeToString())
    return 

def export_onnx_models(lr, xgb, X_train):
    export_LR_to_onnx(lr, X_train)
    export_XG_to_onnx(xgb, X_train)
