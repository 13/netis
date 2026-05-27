"""nmap-based subnet discovery.

Requires `nmap` to be installed and (for ARP probes) root or CAP_NET_RAW.
Uses `-sn` (no port scan) with XML output to capture IPs, MACs, and PTR
hostnames in a single pass.
"""
from __future__ import annotations

import logging
import shutil
import subprocess
import xml.etree.ElementTree as ET
from dataclasses import dataclass

from app.utils.netaddr import normalize_mac

log = logging.getLogger(__name__)


@dataclass
class NmapResult:
    ip_address: str
    mac_address: str | None = None
    hostname: str | None = None


def nmap_scan(cidr: str, timeout: float = 30.0) -> list[NmapResult]:
    """Discover live hosts via `nmap -sn`. Returns empty list if nmap is absent."""
    if not shutil.which("nmap"):
        log.warning("nmap not found — install nmap to use this scan method")
        return []

    # nmap needs wall-clock time proportional to subnet size; honour the
    # caller's budget but never allow less than 30 s so small defaults don't
    # kill a /22.  --max-retries 1 + --max-rtt-timeout cuts per-host probing.
    effective = max(timeout, 30.0)
    try:
        proc = subprocess.run(
            [
                "nmap", "-sn", "-T4",
                "--max-retries", "1",
                "--max-rtt-timeout", "1500ms",
                "-oX", "-",
                cidr,
            ],
            capture_output=True,
            text=True,
            timeout=effective + 10,
        )
    except subprocess.TimeoutExpired:
        log.warning("nmap timed out scanning %s (budget %.0fs)", cidr, effective)
        return []
    except Exception as exc:
        log.warning("nmap error scanning %s: %s", cidr, exc)
        return []

    return _parse_xml(proc.stdout)


def _parse_xml(xml_text: str) -> list[NmapResult]:
    results: list[NmapResult] = []
    try:
        root = ET.fromstring(xml_text)
    except ET.ParseError:
        return results

    for host in root.findall("host"):
        status = host.find("status")
        if status is None or status.get("state") != "up":
            continue

        ip: str | None = None
        mac_raw: str | None = None
        for addr in host.findall("address"):
            atype = addr.get("addrtype", "")
            if atype == "ipv4":
                ip = addr.get("addr")
            elif atype == "mac":
                mac_raw = addr.get("addr")

        if not ip:
            continue

        mac: str | None = None
        if mac_raw:
            try:
                mac = normalize_mac(mac_raw)
            except ValueError:
                pass

        hostname: str | None = None
        for hn in host.findall("hostnames/hostname"):
            if hn.get("type") in ("PTR", "user"):
                hostname = hn.get("name")
                break

        results.append(NmapResult(ip_address=ip, mac_address=mac, hostname=hostname))

    return results
