"""Pulls model bundles from MinIO using the same key layout as go/internal/storage."""
import logging
import shutil
from pathlib import Path

from .config import Settings
from .model_store import MODEL_FILE, SCHEMA_FILE

log = logging.getLogger(__name__)


def make_minio_fetcher(settings: Settings):
    if not settings.minio_endpoint:
        return None

    from minio import Minio
    from minio.error import S3Error

    client = Minio(
        settings.minio_endpoint,
        access_key=settings.minio_access_key,
        secret_key=settings.minio_secret_key,
        secure=settings.minio_secure,
    )

    def fetch(name: str, version: str, dest: Path) -> bool:
        tmp = dest.with_name(dest.name + ".partial")
        try:
            for filename in (MODEL_FILE, SCHEMA_FILE):
                client.fget_object(settings.minio_bucket, f"{name}/{version}/{filename}", str(tmp / filename))
        except S3Error as err:
            log.warning("minio fetch %s/%s failed: %s", name, version, err.code)
            shutil.rmtree(tmp, ignore_errors=True)
            return False
        # Rename only once both files are down, so a half-written bundle is never loaded.
        dest.parent.mkdir(parents=True, exist_ok=True)
        shutil.rmtree(dest, ignore_errors=True)
        tmp.rename(dest)
        return True

    return fetch
