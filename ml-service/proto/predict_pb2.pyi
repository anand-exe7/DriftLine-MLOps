from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional

DESCRIPTOR: _descriptor.FileDescriptor

class PredictionRequest(_message.Message):
    __slots__ = ("model_name", "model_version", "features", "request_id")
    class FeaturesEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: float
        def __init__(self, key: _Optional[str] = ..., value: _Optional[float] = ...) -> None: ...
    MODEL_NAME_FIELD_NUMBER: _ClassVar[int]
    MODEL_VERSION_FIELD_NUMBER: _ClassVar[int]
    FEATURES_FIELD_NUMBER: _ClassVar[int]
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    model_name: str
    model_version: str
    features: _containers.ScalarMap[str, float]
    request_id: str
    def __init__(self, model_name: _Optional[str] = ..., model_version: _Optional[str] = ..., features: _Optional[_Mapping[str, float]] = ..., request_id: _Optional[str] = ...) -> None: ...

class PredictionResponse(_message.Message):
    __slots__ = ("prediction", "probabilities", "model_version_used", "latency_ms", "status_code", "error_message", "request_id")
    PREDICTION_FIELD_NUMBER: _ClassVar[int]
    PROBABILITIES_FIELD_NUMBER: _ClassVar[int]
    MODEL_VERSION_USED_FIELD_NUMBER: _ClassVar[int]
    LATENCY_MS_FIELD_NUMBER: _ClassVar[int]
    STATUS_CODE_FIELD_NUMBER: _ClassVar[int]
    ERROR_MESSAGE_FIELD_NUMBER: _ClassVar[int]
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    prediction: int
    probabilities: _containers.RepeatedScalarFieldContainer[float]
    model_version_used: str
    latency_ms: int
    status_code: int
    error_message: str
    request_id: str
    def __init__(self, prediction: _Optional[int] = ..., probabilities: _Optional[_Iterable[float]] = ..., model_version_used: _Optional[str] = ..., latency_ms: _Optional[int] = ..., status_code: _Optional[int] = ..., error_message: _Optional[str] = ..., request_id: _Optional[str] = ...) -> None: ...

class HealthRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class HealthResponse(_message.Message):
    __slots__ = ("status", "loaded_models")
    STATUS_FIELD_NUMBER: _ClassVar[int]
    LOADED_MODELS_FIELD_NUMBER: _ClassVar[int]
    status: str
    loaded_models: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, status: _Optional[str] = ..., loaded_models: _Optional[_Iterable[str]] = ...) -> None: ...
