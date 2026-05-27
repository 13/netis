"""ICMP ping sweep — reachability only, never authoritative."""

from __future__ import annotations

import concurrent.futures
import subprocess
from dataclasses import dataclass

from app.utils.netaddr import network_hosts


@dataclass
class PingResult:
    ip_address: str
    reachable: bool
    hostname: str | None = None


def _ping_one(host: str, timeout: float) -> PingResult:
    # Send 2 ICMP echoes with a tight interval. The host counts as reachable if
    # at least one reply comes back — robust against single-packet loss on
    # congested or routed links where pings cross multiple hops.
    wait = max(1, int(timeout))
    try:
        res = subprocess.run(
            ["ping", "-c", "2", "-i", "0.2", "-W", str(wait), host],
            capture_output=True,
            timeout=wait * 2 + 1,
        )
        return PingResult(ip_address=host, reachable=res.returncode == 0)
    except (subprocess.TimeoutExpired, FileNotFoundError):
        return PingResult(ip_address=host, reachable=False)


def ping_sweep(cidr: str, timeout: float = 1.0, max_workers: int = 64) -> list[PingResult]:
    hosts = network_hosts(cidr)
    with concurrent.futures.ThreadPoolExecutor(max_workers=max_workers) as ex:
        results = list(ex.map(lambda h: _ping_one(h, timeout), hosts))
    reachable = [r for r in results if r.reachable]
    if reachable:
        from app.scanners.rdns import resolve_all
        hostnames = resolve_all([r.ip_address for r in reachable])
        for r in reachable:
            r.hostname = hostnames.get(r.ip_address)
    return reachable
