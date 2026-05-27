"""DHCP lease parsers — dnsmasq, ISC, generic CSV/JSON."""

from __future__ import annotations

import csv
import json
import re
from dataclasses import dataclass
from io import StringIO

from app.utils.netaddr import normalize_mac, validate_ip


@dataclass
class Lease:
    ip_address: str
    mac_address: str
    hostname: str | None = None


def parse_dnsmasq_leases(text: str) -> list[Lease]:
    """dnsmasq /var/lib/misc/dnsmasq.leases format:
    <expiry> <mac> <ip> <hostname> <client-id>
    """
    out: list[Lease] = []
    for line in text.splitlines():
        parts = line.strip().split()
        if len(parts) < 4:
            continue
        try:
            mac = normalize_mac(parts[1])
            ip = validate_ip(parts[2])
        except ValueError:
            continue
        hostname = parts[3] if parts[3] != "*" else None
        out.append(Lease(ip_address=ip, mac_address=mac, hostname=hostname))
    return out


_ISC_LEASE_RE = re.compile(r"lease\s+(\S+)\s*\{(.*?)\}", re.DOTALL)
_ISC_MAC_RE = re.compile(r"hardware\s+ethernet\s+([0-9a-fA-F:]+)")
_ISC_HOST_RE = re.compile(r'client-hostname\s+"([^"]+)"')


def parse_isc_leases(text: str) -> list[Lease]:
    """ISC dhcpd dhcpd.leases format."""
    out: list[Lease] = []
    for m in _ISC_LEASE_RE.finditer(text):
        ip_raw, body = m.group(1), m.group(2)
        mac_m = _ISC_MAC_RE.search(body)
        if not mac_m:
            continue
        try:
            ip = validate_ip(ip_raw)
            mac = normalize_mac(mac_m.group(1))
        except ValueError:
            continue
        host_m = _ISC_HOST_RE.search(body)
        out.append(
            Lease(ip_address=ip, mac_address=mac, hostname=host_m.group(1) if host_m else None)
        )
    return out


def parse_lease_csv(text: str) -> list[Lease]:
    """CSV with headers: ip,mac,hostname (hostname optional)."""
    out: list[Lease] = []
    reader = csv.DictReader(StringIO(text))
    for row in reader:
        ip_raw = row.get("ip") or row.get("ip_address") or ""
        mac_raw = row.get("mac") or row.get("mac_address") or ""
        if not ip_raw or not mac_raw:
            continue
        try:
            out.append(
                Lease(
                    ip_address=validate_ip(ip_raw.strip()),
                    mac_address=normalize_mac(mac_raw.strip()),
                    hostname=(row.get("hostname") or "").strip() or None,
                )
            )
        except ValueError:
            continue
    return out


def parse_lease_json(text: str) -> list[Lease]:
    """JSON array of {ip, mac, hostname?} entries — supports common UniFi exports."""
    data = json.loads(text)
    if isinstance(data, dict):
        # UniFi controller export: {"data": [...]}
        data = data.get("data", [])
    out: list[Lease] = []
    for entry in data:
        if not isinstance(entry, dict):
            continue
        ip_raw = entry.get("ip") or entry.get("ip_address") or entry.get("fixed_ip")
        mac_raw = entry.get("mac") or entry.get("mac_address")
        if not ip_raw or not mac_raw:
            continue
        try:
            out.append(
                Lease(
                    ip_address=validate_ip(ip_raw),
                    mac_address=normalize_mac(mac_raw),
                    hostname=entry.get("hostname") or entry.get("name"),
                )
            )
        except ValueError:
            continue
    return out
