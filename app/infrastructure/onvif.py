"""ONVIF infrastructure adapter wrapping onvif-python.

Encapsulates all ONVIF wire-protocol calls (connection checks, snapshot
and stream URI resolution, RTSP auth URL building) so services depend on
this adapter rather than the third-party client directly.
"""

from loguru import logger
from onvif import ONVIFClient
from typing import Optional


class ONVIFCameraClient:
    """Adapter that performs ONVIF operations against a remote camera.

    Each method opens a short-lived ONVIFClient connection, so instances
    are stateless and safe to share as a singleton injected into
    SnapshotService.
    """

    def test_connection(self, host: str, port: int, username: str, password: str) -> tuple[bool, list[str], str | None]:
        """Verify ONVIF connectivity and collect media profile tokens.

        Args:
            host: The camera hostname or IP address.
            port: The ONVIF service port (commonly 80 or 8000).
            username: The camera authentication username.
            password: The camera authentication password.

        Returns:
            A tuple of (success flag, profile tokens, error message).
            The error is None when the connection succeeds.
        """
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
        """Resolve the JPEG snapshot URI for a camera profile.

        When profile_token is None, auto-selects the first available media
        profile before requesting the snapshot URI.

        Args:
            host: The camera hostname or IP address.
            port: The ONVIF service port.
            username: The camera authentication username.
            password: The camera authentication password.
            profile_token: Optional media profile token; auto-selected when None.

        Returns:
            A tuple of (snapshot URI, error message). The URI is None on
            failure and the error is None on success.
        """
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
        """Resolve the RTSP unicast stream URI for a given profile.

        Args:
            host: The camera hostname or IP address.
            port: The ONVIF service port.
            username: The camera authentication username.
            password: The camera authentication password.
            profile_token: The media profile token to request the stream for.

        Returns:
            A tuple of (stream URI, error message). The URI is None on
            failure and the error is None on success.
        """
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
        """Find a JPEG-encoded stream URI among available profiles.

        Iterates media profiles and returns the first whose
        VideoEncoderConfiguration uses JPEG encoding.

        Args:
            host: The camera hostname or IP address.
            port: The ONVIF service port.
            username: The camera authentication username.
            password: The camera authentication password.

        Returns:
            A tuple of (JPEG stream URI, profile token, error message).
            Both the URI and token are None when no JPEG profile exists.
        """
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
        """Resolve the stream URI for the first available media profile.

        Args:
            host: The camera hostname or IP address.
            port: The ONVIF service port.
            username: The camera authentication username.
            password: The camera authentication password.

        Returns:
            A tuple of (stream URI, error message). The URI is None when
            no profiles exist or the request fails.
        """
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
        """Resolve the stream URI for the highest-resolution media profile.

        Selects the profile with the largest Width*Height product and
        requests its RTSP unicast stream URI.

        Args:
            host: The camera hostname or IP address.
            port: The ONVIF service port.
            username: The camera authentication username.
            password: The camera authentication password.

        Returns:
            A tuple of (stream URI, profile token, error message). The
            URI and token are None when no profiles exist or the request
            fails.
        """
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
        """Embed credentials into a URI scheme when possible.

        Inserts username:password into the URI authority so callers can
        fetch RTSP or HTTP snapshots without separate auth handling.

        Args:
            raw_uri: The original URI to amend.
            username: The camera authentication username.
            password: The camera authentication password.

        Returns:
            The URI with embedded credentials, or the original URI when
            credentials are empty or the URI has no scheme separator.
        """
        if not raw_uri:
            return raw_uri
        if username and password:
            if "://" in raw_uri:
                scheme, rest = raw_uri.split("://", 1)
                return f"{scheme}://{username}:{password}@{rest}"
        return raw_uri