#!/usr/bin/env bash
# Regenerates gRPC bindings from proto/predict.proto.
#   Go     -> proto/predict/        (needs protoc, protoc-gen-go, protoc-gen-go-grpc on PATH)
#   Python -> ml-service/proto/     (needs grpcio-tools in the active Python env)
set -euo pipefail
cd "$(dirname "$0")/.."

PYTHON="${PYTHON:-python}"

mkdir -p proto/predict ml-service/proto

protoc -I proto \
  --go_out=proto/predict --go_opt=paths=source_relative \
  --go-grpc_out=proto/predict --go-grpc_opt=paths=source_relative \
  proto/predict.proto

# -I . keeps the "proto/" prefix, so the generated grpc stub imports
# "from proto import predict_pb2" which resolves when ml-service/ is the CWD.
"$PYTHON" -m grpc_tools.protoc -I . \
  --python_out=ml-service --pyi_out=ml-service --grpc_python_out=ml-service \
  proto/predict.proto
touch ml-service/proto/__init__.py

echo "generated: proto/predict/*.go, ml-service/proto/*.py"
