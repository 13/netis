"""Reverse DNS hostname resolution.

Tries in order:
1. socket.gethostbyaddr — stdlib, no subprocess, fastest
2. host CLI            — common on Linux (bind-utils / dnsutils)
3. nslookup CLI        — fallback when host is absent
"""
from __future__ import annotations

import concurrent.futures
import logging
import shutil
import socket
import subprocess

log = logging.getLogger(__name__)


def resolve_hostname(ip: str) -> str | None:
    """Reverse-DNS lookup for a single IP. Returns the hostname or None."""
    try:
        hostname, _, _ = socket.gethostbyaddr(ip)
        return hostname or None
    except (socket.herror, socket.gaierror, OSError):
        pass

    if shutil.which("host"):
        try:
            res = subprocess.run(
                ["host", ip],
                capture_output=True, text=True, timeout=5,
            )
            for line in res.stdout.splitlines():
                # "1.1.168.192.in-addr.arpa domain name pointer myhost.local."
                if "domain name pointer" in line:
                    return line.split("pointer", 1)[-1].strip().rstrip(".")
        except (subprocess.TimeoutExpired, FileNotFoundError):
            pass

    if shutil.which("nslookup"):
        try:
            res = subprocess.run(
                ["nslookup", ip],
                capture_output=True, text=True, timeout=5,
            )
            for line in res.stdout.splitlines():
                # "name = myhost.local."
                stripped = line.strip()
                if stripped.startswith("name ="):
                    return stripped.split("=", 1)[-1].strip().rstrip(".")
        except (subprocess.TimeoutExpired, FileNotFoundError):
            pass

    return None


def resolve_all(ips: list[str], max_workers: int = 32) -> dict[str, str | None]:
    """Resolve multiple IPs concurrently. Returns {ip: hostname_or_None}."""
    if not ips:
        return {}
    with concurrent.futures.ThreadPoolExecutor(max_workers=min(max_workers, len(ips))) as ex:
        results = list(ex.map(resolve_hostname, ips))
    return dict(zip(ips, results))
