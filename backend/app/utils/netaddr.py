"""Network address helpers.

Use only stdlib `ipaddress` — no third-party netaddr dep needed.
"""

from __future__ import annotations

import ipaddress
import re

_MAC_RE = re.compile(r"^[0-9a-f]{12}$")


def validate_ip(value: str) -> str:
    try:
        return str(ipaddress.ip_address(value))
    except ValueError as exc:
        raise ValueError(f"invalid IP address: {value!r}") from exc


def validate_cidr(value: str) -> str:
    try:
        net = ipaddress.ip_network(value, strict=False)
    except ValueError as exc:
        raise ValueError(f"invalid CIDR: {value!r}") from exc
    return str(net)


def network_hosts(cidr: str) -> list[str]:
    """Return all usable host IPs for a CIDR.

    For IPv4 /31 and /32 we treat all addresses as hosts (RFC 3021 / single).
    """
    net = ipaddress.ip_network(cidr, strict=False)
    if isinstance(net, ipaddress.IPv4Network) and net.prefixlen >= 31:
        return [str(ip) for ip in net]
    return [str(ip) for ip in net.hosts()]


def ip_in_subnet(ip: str, cidr: str) -> bool:
    try:
        return ipaddress.ip_address(ip) in ipaddress.ip_network(cidr, strict=False)
    except ValueError:
        return False


def normalize_mac(value: str) -> str:
    """Normalize a MAC address to lowercase colon-separated form (aa:bb:cc:dd:ee:ff)."""
    if not value:
        raise ValueError("empty MAC")
    raw = re.sub(r"[^0-9a-fA-F]", "", value).lower()
    if not _MAC_RE.match(raw):
        raise ValueError(f"invalid MAC address: {value!r}")
    return ":".join(raw[i : i + 2] for i in range(0, 12, 2))
