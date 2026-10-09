# PowerShell equivalent of gen_proto.sh
$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")
$python = if ($env:PYTHON) { $env:PYTHON } else { "python" }

New-Item -ItemType Directory -Force proto/predict, ml-service/proto | Out-Null

protoc -I proto `
  --go_out=proto/predict --go_opt=paths=source_relative `
  --go-grpc_out=proto/predict --go-grpc_opt=paths=source_relative `
  proto/predict.proto
if ($LASTEXITCODE -ne 0) { throw "protoc (go) failed" }

& $python -m grpc_tools.protoc -I . `
  --python_out=ml-service --pyi_out=ml-service --grpc_python_out=ml-service `
  proto/predict.proto
if ($LASTEXITCODE -ne 0) { throw "grpc_tools.protoc (python) failed" }

if (-not (Test-Path ml-service/proto/__init__.py)) { New-Item -ItemType File ml-service/proto/__init__.py | Out-Null }
Write-Host "generated: proto/predict/*.go, ml-service/proto/*.py"
