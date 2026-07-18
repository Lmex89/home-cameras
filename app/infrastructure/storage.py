"""S3-compatible storage adapter for uploading large video files.

Uploads timelapse videos to Backblaze B2 (or any S3-compatible provider)
when they exceed Telegram's 50 MB bot upload limit, then sends the
public URL via Telegram.
"""

import asyncio
from pathlib import Path
from typing import TYPE_CHECKING

from loguru import logger

from app.core.config import settings

if TYPE_CHECKING or True:
    import boto3
    from botocore.exceptions import ClientError


class StorageProvider:
    """Upload files to an S3-compatible object store.

    Wraps ``boto3`` behind a thin adapter so the rest of the application
    depends on this class rather than the SDK directly.
    """

    def __init__(
        self,
        endpoint_url: str,
        bucket_name: str,
        access_key: str,
        secret_key: str,
        public_url: str,
        region: str = "us-west-004",
    ) -> None:
        self._endpoint_url = endpoint_url
        self._bucket = bucket_name
        self._public_url = public_url
        self._client = boto3.client(
            "s3",
            endpoint_url=endpoint_url,
            aws_access_key_id=access_key,
            aws_secret_access_key=secret_key,
            region_name=region,
        )

    @classmethod
    def from_settings(cls) -> "StorageProvider | None":
        """Build an instance from application settings.

        Returns ``None`` when ``settings.storage_enabled`` is ``False``
        or any required config is missing.
        """
        if not settings.storage_enabled:
            logger.debug("Storage disabled, will use local fallback URL")
            return None
        missing = [
            k
            for k, v in {
                "STORAGE_ENDPOINT_URL": settings.storage_endpoint_url,
                "STORAGE_BUCKET_NAME": settings.storage_bucket_name,
                "STORAGE_ACCESS_KEY": settings.storage_access_key,
                "STORAGE_SECRET_KEY": settings.storage_secret_key,
                "STORAGE_PUBLIC_URL": settings.storage_public_url,
            }.items()
            if not v
        ]
        if missing:
            logger.warning(f"Storage enabled but missing config: {', '.join(missing)}")
            return None
        logger.info(
            f"Storage configured: endpoint={settings.storage_endpoint_url} "
            f"bucket={settings.storage_bucket_name}"
        )
        return cls(
            endpoint_url=settings.storage_endpoint_url,
            bucket_name=settings.storage_bucket_name,
            access_key=settings.storage_access_key,
            secret_key=settings.storage_secret_key,
            public_url=settings.storage_public_url,
            region=settings.storage_region,
        )

    async def upload(self, local_path: Path) -> str | None:
        """Upload a file to the configured bucket.

        Runs the actual boto3 upload in a thread to avoid blocking the
        event loop.

        Args:
            local_path: Absolute path to the local file to upload.

        Returns:
            The public URL of the uploaded file, or ``None`` on failure.
        """
        key = f"videos/{local_path.name}"
        logger.info(f"Uploading {local_path} to s3://{self._bucket}/{key}")
        try:
            await asyncio.to_thread(
                self._client.upload_file,
                str(local_path),
                self._bucket,
                key,
                ExtraArgs={"ContentType": "video/mp4", "ACL": "public-read"},
            )
            url = f"{self._public_url}/{key}"
            size_mb = local_path.stat().st_size / (1024 * 1024)
            logger.info(f"Uploaded {local_path} ({size_mb:.1f} MB) to {url}")
            return url
        except ClientError:
            logger.exception(f"Failed to upload {local_path} to S3")
            return None
