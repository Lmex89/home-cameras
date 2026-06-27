from loguru import logger
from onvif import ONVIFClient
from typing import Optional


class ONVIFCameraClient:
    def test_connection(self, host: str, port: int, username: str, password: str) -> tuple[bool, list[str], str | None]:
        logger.debug(f"Testing ONVIF connection to {host}:{port}")
        try:
            client = ONVIFClient(host, port, username, password, timeout=10)
            device = client.devicemgmt()
            info = device.GetDeviceInformation()
            logger.debug(f"Device info for {host}:{port} — {info}")
            media = client.media()
            profiles = media.GetProfiles()
            profile_tokens = [p.token for p in profiles]
            logger.info(f"ONVIF connection OK to {host}:{port} ({len(profile_tokens)} profiles)")
            return True, profile_tokens, None
        except Exception as e:
            logger.warning(f"ONVIF connection failed to {host}:{port} — {e}")
            return False, [], str(e)

    def get_snapshot_uri(
        self, host: str, port: int, username: str, password: str, profile_token: str | None = None
    ) -> tuple[str | None, str | None]:
        logger.debug(f"Getting snapshot URI from {host}:{port} (profile: {profile_token})")
        try:
            client = ONVIFClient(host, port, username, password, timeout=15)
            media = client.media()
            if profile_token is None:
                profiles = media.GetProfiles()
                if not profiles:
                    logger.warning(f"No media profiles found for {host}:{port}")
                    return None, "No media profiles found"
                profile_token = profiles[0].token
                logger.debug(f"Auto-selected profile {profile_token} for {host}:{port}")
            snapshot = media.GetSnapshotUri(ProfileToken=profile_token)
            uri = snapshot.Uri
            if isinstance(uri, dict):
                uri = uri.get("Uri", "")
            logger.debug(f"Snapshot URI resolved: {uri}")
            return str(uri), None
        except Exception as e:
            logger.error(f"Failed to get snapshot URI from {host}:{port} — {e}")
            return None, str(e)

    def get_stream_uri(
        self, host: str, port: int, username: str, password: str, profile_token: str
    ) -> tuple[Optional[str], Optional[str]]:
        logger.debug(f"Getting stream URI from {host}:{port} (profile: {profile_token})")
        try:
            client = ONVIFClient(host, port, username, password, timeout=15)
            media = client.media()
            result = media.GetStreamUri(
                {'Stream': 'RTP-Unicast', 'Transport': {'Protocol': 'RTSP'}},
                profile_token,
            )
            uri = str(result.Uri)
            logger.debug(f"Stream URI resolved: {uri}")
            return uri, None
        except Exception as e:
            logger.warning(f"Failed to get stream URI from {host}:{port} — {e}")
            return None, str(e)

    def get_jpeg_stream_uri(
        self, host: str, port: int, username: str, password: str
    ) -> tuple[Optional[str], Optional[str], Optional[str]]:
        logger.debug(f"Looking for JPEG stream on {host}:{port}")
        try:
            client = ONVIFClient(host, port, username, password, timeout=15)
            media = client.media()
            profiles = media.GetProfiles()
            for p in profiles:
                if p.VideoEncoderConfiguration and p.VideoEncoderConfiguration.Encoding == 'JPEG':
                    uri, err = self.get_stream_uri(host, port, username, password, p.token)
                    if uri:
                        logger.info(f"Found JPEG stream: {uri} (profile: {p.token})")
                        return uri, p.token, None
            return None, None, "No JPEG stream profile found"
        except Exception as e:
            logger.warning(f"Failed to find JPEG stream on {host}:{port} — {e}")
            return None, None, str(e)

    def get_first_stream_uri(
        self, host: str, port: int, username: str, password: str
    ) -> tuple[Optional[str], Optional[str]]:
        logger.debug(f"Getting first available stream URI from {host}:{port}")
        try:
            client = ONVIFClient(host, port, username, password, timeout=15)
            media = client.media()
            profiles = media.GetProfiles()
            if not profiles:
                return None, "No media profiles found"
            uri, err = self.get_stream_uri(host, port, username, password, profiles[0].token)
            if uri:
                logger.info(f"First stream: {uri} (profile: {profiles[0].token})")
            return uri, err
        except Exception as e:
            return None, str(e)

    def get_best_stream_uri(
        self, host: str, port: int, username: str, password: str
    ) -> tuple[Optional[str], Optional[str], Optional[str]]:
        logger.debug(f"Finding best stream URI from {host}:{port}")
        try:
            client = ONVIFClient(host, port, username, password, timeout=15)
            media = client.media()
            profiles = media.GetProfiles()
            if not profiles:
                return None, None, "No media profiles found"

            best = max(
                profiles,
                key=lambda p: (
                    p.VideoEncoderConfiguration.Resolution.Width * p.VideoEncoderConfiguration.Resolution.Height
                    if p.VideoEncoderConfiguration and p.VideoEncoderConfiguration.Resolution
                    else 0
                ),
            )
            uri, err = self.get_stream_uri(host, port, username, password, best.token)
            if uri:
                logger.info(f"Best stream: {uri} (profile: {best.token})")
            return uri, best.token, err
        except Exception as e:
            return None, None, str(e)

    def build_auth_url(self, raw_uri: str, username: str, password: str) -> str:
        if not raw_uri:
            return raw_uri
        if username and password:
            if "://" in raw_uri:
                scheme, rest = raw_uri.split("://", 1)
                return f"{scheme}://{username}:{password}@{rest}"
        return raw_uri
