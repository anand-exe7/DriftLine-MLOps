import os
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class Settings:
    model_dir: Path
    grpc_port: int
    http_port: int
    grpc_workers: int
    # Optional MinIO fallback: when a model isn't on local disk, pull it from the
    # bucket the Go registry uploads to. Leave MINIO_ENDPOINT empty to disable.
    minio_endpoint: str
    minio_access_key: str
    minio_secret_key: str
    minio_bucket: str
    minio_secure: bool


def load_settings() -> Settings:
    here = Path(__file__).resolve().parents[1]
    return Settings(
        model_dir=Path(os.getenv("MODEL_DIR", str(here / "models"))),
        grpc_port=int(os.getenv("GRPC_PORT", "50051")),
        http_port=int(os.getenv("HTTP_PORT", "8000")),
        grpc_workers=int(os.getenv("GRPC_WORKERS", "8")),
        minio_endpoint=os.getenv("MINIO_ENDPOINT", ""),
        minio_access_key=os.getenv("MINIO_ACCESS_KEY", ""),
        minio_secret_key=os.getenv("MINIO_SECRET_KEY", ""),
        minio_bucket=os.getenv("MINIO_BUCKET", "driftline-artifacts"),
        minio_secure=os.getenv("MINIO_SECURE", "false").lower() == "true",
    )
