"""Shared scan execution: run a scan, record observations, fire change alerts.

Used by both the synchronous /discovery/scan endpoint and the background job
manager so the behaviour is identical regardless of how a scan is triggered.
"""
from __future__ import annotations

import logging

from sqlalchemy.orm import Session

from app.models import ObservationSource, Subnet
from app.scanners import arp_scan, nmap_scan, ping_sweep
from app.services.notify import notify_new_unknowns
from app.services.observations import record_observation
from app.services.reconcile import unknown_devices

log = logging.getLogger(__name__)


def _host_key(h: dict) -> str:
    return h.get("mac_address") or h.get("ip_address") or ""


def run_scan(
    db: Session,
    subnet: Subnet,
    method: str = "arp",
    timeout: float = 2.0,
    *,
    alert: bool = True,
) -> int:
    """Scan a subnet, record observations, return the number of hosts found.

    When ``alert`` is set, any host that became "unknown" as a result of this
    scan triggers a notification.
    """
    before = {_host_key(h) for h in unknown_devices(db)} if alert else set()

    found = 0
    if method == "arp":
        for r in arp_scan(subnet.cidr, timeout=timeout):
            record_observation(
                db, ip_address=r.ip_address, mac_address=r.mac_address,
                hostname=r.hostname, source=ObservationSource.arp,
            )
            found += 1
    elif method == "nmap":
        for r in nmap_scan(subnet.cidr, timeout=timeout):
            record_observation(
                db, ip_address=r.ip_address, mac_address=r.mac_address,
                hostname=r.hostname, source=ObservationSource.nmap,
            )
            found += 1
    else:
        for r in ping_sweep(subnet.cidr, timeout=timeout):
            record_observation(
                db, ip_address=r.ip_address, mac_address=None,
                hostname=r.hostname, source=ObservationSource.ping,
            )
            found += 1

    db.commit()

    if alert:
        new_hosts = [h for h in unknown_devices(db) if _host_key(h) not in before]
        if new_hosts:
            try:
                notify_new_unknowns(new_hosts, subnet=subnet)
            except Exception:  # noqa: BLE001 — alerting must never break a scan
                log.exception("new-unknown notification failed")

    return found
