"""Pi-hole v6 API client — fetches DHCP leases and local DNS records."""

from __future__ import annotations

from dataclasses import dataclass

import httpx

from app.scanners.leases import Lease
from app.utils.netaddr import normalize_mac, validate_ip


@dataclass
class PiholeConfig:
    url: str       # base URL, e.g. "http://192.168.1.1" (no trailing slash)
    password: str


@dataclass
class LocalDnsRecord:
    ip: str
    hostname: str


class PiholeAuthError(RuntimeError):
    pass


class PiholeApiError(RuntimeError):
    pass


def _auth(config: PiholeConfig) -> str:
    """Authenticate with Pi-hole v6 and return a session SID."""
    try:
        resp = httpx.post(
            f"{config.url}/api/auth",
            json={"password": config.password},
            timeout=10,
        )
    except httpx.RequestError as exc:
        raise PiholeApiError(f"Cannot reach Pi-hole: {exc}") from exc

    if resp.status_code in (401, 403):
        raise PiholeAuthError("Pi-hole authentication failed — check your password")
    if resp.status_code != 200:
        raise PiholeApiError(f"Pi-hole /api/auth returned HTTP {resp.status_code}")

    try:
        sid = resp.json()["session"]["sid"]
    except (KeyError, TypeError, ValueError) as exc:
        raise PiholeAuthError("Pi-hole authentication failed — unexpected response format") from exc

    if not sid:
        raise PiholeAuthError("Pi-hole returned an empty session ID")

    return sid


class PiholeNotFoundError(PiholeApiError):
    pass


def _get(sid: str, url: str) -> dict:
    """GET a Pi-hole v6 API endpoint with the session header."""
    try:
        resp = httpx.get(url, headers={"X-FTL-SID": sid}, timeout=10)
    except httpx.RequestError as exc:
        raise PiholeApiError(f"Cannot reach Pi-hole: {exc}") from exc

    if resp.status_code == 401:
        raise PiholeAuthError("Pi-hole session expired or invalid")
    if resp.status_code == 404:
        raise PiholeNotFoundError(f"Pi-hole endpoint not found: {url}")
    if resp.status_code != 200:
        raise PiholeApiError(f"Pi-hole returned HTTP {resp.status_code} for {url}")

    try:
        return resp.json()
    except ValueError as exc:
        raise PiholeApiError("Pi-hole returned non-JSON response") from exc


def _logout(config: PiholeConfig, sid: str) -> None:
    """Best-effort session logout."""
    try:
        httpx.delete(f"{config.url}/api/auth", headers={"X-FTL-SID": sid}, timeout=5)
    except Exception:
        pass


def fetch_network_devices(config: PiholeConfig) -> list[Lease]:
    """Return active DHCP leases from Pi-hole v6."""
    sid = _auth(config)
    try:
        data = _get(sid, f"{config.url}/api/dhcp/leases")
    finally:
        _logout(config, sid)

    out: list[Lease] = []
    for entry in data.get("leases", []):
        ip_raw = entry.get("ip", "")
        mac_raw = entry.get("hwaddr") or entry.get("mac", "")
        name = entry.get("name") or entry.get("hostname") or ""
        hostname = name if name and name != "*" else None

        try:
            ip = validate_ip(ip_raw)
        except ValueError:
            continue
        try:
            mac = normalize_mac(mac_raw)
        except ValueError:
            continue

        out.append(Lease(ip_address=ip, mac_address=mac, hostname=hostname))
    return out


def fetch_local_dns(config: PiholeConfig) -> list[LocalDnsRecord]:
    """Return custom local DNS A-records from Pi-hole v6. Returns [] if endpoint unavailable."""
    sid = _auth(config)
    try:
        data = _get(sid, f"{config.url}/api/dns/customRecords")
    except PiholeNotFoundError:
        return []
    finally:
        _logout(config, sid)

    out: list[LocalDnsRecord] = []
    for entry in data.get("customRecords", []):
        ip_raw = entry.get("ip", "")
        hostname = entry.get("domain", "")
        if not ip_raw or not hostname:
            continue
        try:
            ip = validate_ip(ip_raw)
        except ValueError:
            continue
        out.append(LocalDnsRecord(ip=ip, hostname=hostname))
    return out
