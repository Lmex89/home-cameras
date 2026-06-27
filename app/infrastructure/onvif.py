from loguru import logger
from onvif import ONVIFClient


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

    def build_auth_url(self, raw_uri: str, username: str, password: str) -> str:
        if not raw_uri:
            return raw_uri
        if username and password:
            if "://" in raw_uri:
                scheme, rest = raw_uri.split("://", 1)
                return f"{scheme}://{username}:{password}@{rest}"
        return raw_uri
