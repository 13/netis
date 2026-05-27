"""ARP-based subnet discovery.

Tries multiple methods in order:
1. scapy    — fast parallel ARP, requires raw-socket privileges
2. arp-scan — whole-subnet ARP CLI, requires privileges (parallel, fast)
3. arping   — per-host ARP CLI, requires privileges (sequential, slow)
4. arp -n   — reads the kernel ARP cache (passive, no privileges, may be stale)

If none are available the scanner returns an empty list rather than raising.
"""

from __future__ import annotations

import ipaddress
import logging
import shutil
import subprocess
from dataclasses import dataclass

from app.utils.netaddr import network_hosts, normalize_mac
from app.utils.oui import lookup_vendor

log = logging.getLogger(__name__)


@dataclass
class ArpResult:
    ip_address: str
    mac_address: str
    mac_vendor: str | None = None
    hostname: str | None = None


def arp_scan(cidr: str, timeout: float = 2.0) -> list[ArpResult]:
    """Scan a CIDR for live hosts via ARP, then resolve hostnames in parallel."""
    results = _arp_scan_raw(cidr, timeout)
    if results:
        from app.scanners.rdns import resolve_all
        hostnames = resolve_all([r.ip_address for r in results])
        for r in results:
            r.hostname = hostnames.get(r.ip_address)
    return results


def _arp_scan_raw(cidr: str, timeout: float) -> list[ArpResult]:
    """Run the ARP scan without hostname resolution."""
    try:
        results = _scan_scapy(cidr, timeout)
        if results:
            return results
        log.debug("scapy returned no results for %s; falling back to CLI tools", cidr)
    except PermissionError as exc:
        log.debug("scapy requires elevated privileges (%s); falling back to CLI tools", exc)
    except Exception as exc:  # noqa: BLE001 — scapy errors are varied
        log.warning("scapy arp_scan failed unexpectedly (%s); falling back to CLI tools", exc)

    if shutil.which("arp-scan"):
        log.info("using arp-scan CLI for %s", cidr)
        return _scan_arp_scan_cli(cidr, timeout)

    if shutil.which("arping"):
        return _scan_arping(cidr, timeout)

    if shutil.which("arp"):
        log.info("no active ARP scanner available; reading kernel ARP cache (passive, may be stale)")
        return _scan_arp_cache(cidr)

    log.warning("no ARP scanner available — scapy failed and no fallback installed")
    return []


def _scan_scapy(cidr: str, timeout: float) -> list[ArpResult]:
    from scapy.all import ARP, Ether, srp  # type: ignore[import-untyped]

    pkt = Ether(dst="ff:ff:ff:ff:ff:ff") / ARP(pdst=cidr)
    ans, _ = srp(pkt, timeout=timeout, verbose=False)
    out: list[ArpResult] = []
    for _, rcv in ans:
        try:
            mac = normalize_mac(rcv.hwsrc)
            out.append(ArpResult(ip_address=rcv.psrc, mac_address=mac, mac_vendor=lookup_vendor(mac)))
        except ValueError:
            continue
    return out


def _scan_arp_scan_cli(cidr: str, timeout: float) -> list[ArpResult]:
    """Use the `arp-scan` binary (active, requires privileges)."""
    out: list[ArpResult] = []
    try:
        res = subprocess.run(
            ["arp-scan", "--retry=2", f"--timeout={int(max(1, timeout) * 1000)}", cidr],
            capture_output=True,
            text=True,
            timeout=timeout + 10,
        )
    except (subprocess.TimeoutExpired, FileNotFoundError):
        return out
    for line in res.stdout.splitlines():
        parts = line.split("\t")
        if len(parts) < 2:
            continue
        ip_raw, mac_raw = parts[0].strip(), parts[1].strip()
        if not ip_raw or not ip_raw[0].isdigit():
            continue
        try:
            mac = normalize_mac(mac_raw)
            out.append(ArpResult(ip_address=ip_raw, mac_address=mac, mac_vendor=lookup_vendor(mac)))
        except ValueError:
            pass
    return out


def _scan_arp_cache(cidr: str) -> list[ArpResult]:
    """Read the kernel ARP cache via `arp -n` (passive, no privileges required)."""
    try:
        net = ipaddress.ip_network(cidr, strict=False)
    except ValueError:
        return []
    out: list[ArpResult] = []
    try:
        res = subprocess.run(["arp", "-n"], capture_output=True, text=True, timeout=5)
    except (subprocess.TimeoutExpired, FileNotFoundError):
        return []
    for line in res.stdout.splitlines():
        parts = line.split()
        # Table format: Address HWtype HWaddress Flags Mask Iface
        if len(parts) < 3 or parts[0] == "Address":
            continue
        ip_raw, hwtype, hwaddr = parts[0], parts[1], parts[2]
        if hwtype != "ether" or hwaddr == "<incomplete>":
            continue
        try:
            if ipaddress.ip_address(ip_raw) in net:
                mac = normalize_mac(hwaddr)
                out.append(ArpResult(ip_address=ip_raw, mac_address=mac, mac_vendor=lookup_vendor(mac)))
        except ValueError:
            continue
    return out


def _scan_arping(cidr: str, timeout: float) -> list[ArpResult]:
    out: list[ArpResult] = []
    for host in network_hosts(cidr):
        try:
            res = subprocess.run(
                ["arping", "-c", "1", "-w", str(int(max(1, timeout))), host],
                capture_output=True,
                text=True,
                timeout=timeout + 1,
            )
        except (subprocess.TimeoutExpired, FileNotFoundError):
            continue
        for line in res.stdout.splitlines():
            line = line.strip()
            # Typical: "Unicast reply from 192.168.1.1 [aa:bb:cc:dd:ee:ff]  0.847ms"
            if "[" in line and "]" in line:
                mac_raw = line.split("[", 1)[1].split("]", 1)[0]
                try:
                    mac = normalize_mac(mac_raw)
                    out.append(ArpResult(ip_address=host, mac_address=mac, mac_vendor=lookup_vendor(mac)))
                except ValueError:
                    pass
                break
    return out
