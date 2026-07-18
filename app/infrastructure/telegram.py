"""Telegram notification adapter for sending video reports.

Sends annotated timelapse videos directly into a Telegram chat using
the ``python-telegram-bot`` library.  Gracefully no-ops when the bot
token or chat ID are not configured.  For videos exceeding 50 MB,
uploads to an S3-compatible storage provider and sends the public URL.
"""

from __future__ import annotations

from pathlib import Path
from typing import TYPE_CHECKING

from loguru import logger

from app.core.config import settings

if TYPE_CHECKING:
    from app.infrastructure.storage import StorageProvider

try:
    import telegram

    _HAS_TELEGRAM = True
except ImportError:  # pragma: no cover
    _HAS_TELEGRAM = False


_UPLOAD_LIMIT = 50 * 1024 * 1024  # 50 MB — telegram.constants.FileSizeLimit.FILESIZE_UPLOAD


class TelegramNotifier:
    """Send messages and video files to a Telegram chat.

    Adapter over ``python-telegram-bot`` that encapsulates all Telegram
    wire calls so the rest of the application depends on this thin adapter
    rather than the third-party library directly.

    When ``settings.telegram_enabled`` is ``False``, or when the bot token /
    chat id are empty, every method is a no-op (logs at DEBUG level).

    When the video exceeds 50 MB, the notifier tries to upload it to an
    S3-compatible provider and sends the public URL as a text message.
    Falls back to the local download URL if no storage is configured.
    """

    def __init__(self, token: str, chat_id: str, storage: StorageProvider | None = None) -> None:
        self._token = token
        self._chat_id = chat_id
        self._storage = storage

    @classmethod
    def from_settings(cls) -> TelegramNotifier:
        """Build an instance from application settings.

        Also initialises a ``StorageProvider`` when storage is enabled
        so that oversize videos are uploaded automatically.

        Returns:
            A new ``TelegramNotifier`` wired to the current config.
        """
        storage: StorageProvider | None = None
        if settings.storage_enabled:
            from app.infrastructure.storage import StorageProvider as SP
            storage = SP.from_settings()
        return cls(settings.telegram_bot_token, settings.telegram_chat_id, storage=storage)

    def _enabled(self) -> bool:
        """Check whether Telegram notifications are configured and enabled."""
        return bool(
            settings.telegram_enabled
            and self._token
            and self._chat_id
            and _HAS_TELEGRAM
        )

    async def send_message(self, text: str) -> bool:
        """Send a plain text message to the configured chat.

        Args:
            text: The message body.

        Returns:
            ``True`` when the message was sent successfully, ``False``
            otherwise (or when the notifier is disabled).
        """
        if not self._enabled():
            logger.debug(f"Telegram disabled; would send message: {text[:80]}…")
            return False
        logger.info(f"Sending Telegram message: {text[:80]}…")
        try:
            bot = telegram.Bot(self._token)
            async with bot:
                await bot.send_message(chat_id=self._chat_id, text=text)
            return True
        except Exception:
            logger.exception("Failed to send Telegram message")
            return False

    async def send_video(
        self,
        path: Path,
        caption: str,
        fallback_url: str | None = None,
        public_url: str | None = None,
    ) -> bool:
        """Send a video file to the configured chat.

        If the file exceeds the 50 MB bot upload limit, tries to upload
        it to the configured S3-compatible storage provider and sends the
        public URL as a text message.  When storage is not configured or
        the upload fails, uses ``fallback_url`` as the download link.

        When the video is under the limit, the video is sent directly and
        a separate text message with the download URL is also sent so that
        the user always has a clickable link alongside the video preview.

        Args:
            path: Absolute filesystem path to the MP4 video.
            caption: Short description (max 1024 chars, shown under the
                video in the chat).
            fallback_url: Optional download URL for when the video exceeds
                the upload limit and no storage is configured.
            public_url: Optional public URL to send as a text message even
                when the video fits the upload limit.  Takes precedence
                over ``fallback_url``.

        Returns:
            ``True`` when the video (or a fallback message) was sent
            successfully, ``False`` otherwise.
        """
        if not self._enabled():
            logger.debug(f"Telegram disabled; would send video: {path}")
            return False

        size = path.stat().st_size if path.exists() else 0

        if size > _UPLOAD_LIMIT:
            logger.warning(
                f"Video {path} is {size / 1024 / 1024:.1f} MB, "
                f"exceeds 50 MB Telegram limit"
            )
            public_url_s3: str | None = public_url
            if not public_url_s3 and self._storage:
                logger.info("Attempting upload to S3-compatible storage")
                public_url_s3 = await self._storage.upload(path)

            link = public_url_s3 or fallback_url or caption
            msg = f"{caption}\n\n📹 Download: {link}"
            return await self.send_message(msg)

        logger.info(f"Sending video {path} ({size / 1024 / 1024:.1f} MB) to Telegram chat")
        try:
            bot = telegram.Bot(self._token)
            async with bot:
                with open(path, "rb") as fh:
                    await bot.send_video(
                        chat_id=self._chat_id,
                        video=fh,
                        caption=caption,
                        supports_streaming=True,
                        filename=path.name,
                        write_timeout=120,
                        read_timeout=120,
                    )
        except Exception:
            logger.exception(f"Failed to send video {path}")
            return False

        # Always send a text message with the download URL as well, so the
        # user has a clickable link even when the video was sent directly.
        link = public_url or fallback_url
        if link:
            msg = f"{caption}\n\n📹 Download: {link}"
            return await self.send_message(msg)

        return True
