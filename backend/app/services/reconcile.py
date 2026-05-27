"""Reconciliation between intended (authoritative) state and observed reality.

Critical design rule: never automatically overwrite authoritative assignments
using scan data. Observations live in their own table and are surfaced
alongside IPAddress records for the UI to reconcile.
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime

from sqlalchemy import select
from sqlalchemy.orm import Session

from app.models import IPAddress, IPStatus, Observation, Subnet
from app.utils.netaddr import ip_in_subnet, network_hosts
from app.utils.oui import lookup_vendor

AUTHORITATIVE_STATUSES = {IPStatus.reserved, IPStatus.static, IPStatus.dhcp}


@dataclass
class IPRow:
    """A single row in a subnet view, merging intended + observed state."""

    ip_address: str
    status: IPStatus
    assignment_id: int | None
    device_id: int | None
    description: str | None
    observed_mac: str | None
    observed_hostname: str | None
    last_seen: datetime | None
    conflict: bool


def subnet_rows(db: Session, subnet: Subnet) -> list[IPRow]:
    """Build the merged view of a subnet — every host IP with its status."""
    hosts = network_hosts(subnet.cidr)

    ips = db.execute(
        select(IPAddress).where(IPAddress.subnet_id == subnet.id)
    ).scalars().all()
    ip_by_addr: dict[str, IPAddress] = {ip.ip_address: ip for ip in ips}

    obs_rows = db.execute(
        select(Observation).where(Observation.ip_address.in_(hosts))
    ).scalars().all()

    # Most recent observation per IP (for last_seen / hostname / source).
    obs_by_addr: dict[str, Observation] = {}
    # Most recent MAC-bearing observation per IP — a later ping scan must not
    # erase a MAC that was captured by an earlier ARP scan.
    mac_obs_by_addr: dict[str, Observation] = {}
    for o in obs_rows:
        cur = obs_by_addr.get(o.ip_address)
        if cur is None or o.last_seen > cur.last_seen:
            obs_by_addr[o.ip_address] = o
        if o.mac_address:
            cur_mac = mac_obs_by_addr.get(o.ip_address)
            if cur_mac is None or o.last_seen > cur_mac.last_seen:
                mac_obs_by_addr[o.ip_address] = o

    rows: list[IPRow] = []
    for host in hosts:
        ip = ip_by_addr.get(host)
        obs = obs_by_addr.get(host)
        mac_obs = mac_obs_by_addr.get(host)
        observed_mac = mac_obs.mac_address if mac_obs else None

        if ip is None and obs is None:
            rows.append(
                IPRow(
                    ip_address=host,
                    status=IPStatus.free,
                    assignment_id=None,
                    device_id=None,
                    description=None,
                    observed_mac=None,
                    observed_hostname=None,
                    last_seen=None,
                    conflict=False,
                )
            )
            continue

        if ip is None:
            rows.append(
                IPRow(
                    ip_address=host,
                    status=IPStatus.observed,
                    assignment_id=None,
                    device_id=None,
                    description=None,
                    observed_mac=observed_mac,
                    observed_hostname=obs.hostname if obs else None,
                    last_seen=obs.last_seen if obs else None,
                    conflict=False,
                )
            )
            continue

        conflict = False
        if (
            ip.status in AUTHORITATIVE_STATUSES
            and ip.device_id is not None
            and observed_mac is not None
        ):
            from app.models import Device  # local import

            device = db.get(Device, ip.device_id)
            if device and device.mac_address and device.mac_address != observed_mac:
                conflict = True

        rows.append(
            IPRow(
                ip_address=host,
                status=IPStatus.conflict if conflict else ip.status,
                assignment_id=ip.id,
                device_id=ip.device_id,
                description=ip.description,
                observed_mac=observed_mac,
                observed_hostname=obs.hostname if obs else None,
                last_seen=obs.last_seen if obs else ip.last_seen,
                conflict=conflict,
            )
        )
    return rows


def subnet_stats(db: Session, subnet: Subnet) -> dict:
    rows = subnet_rows(db, subnet)
    return {
        "total_ips": len(rows),
        "assigned_ips": sum(1 for r in rows if r.status in AUTHORITATIVE_STATUSES),
        "free_ips": sum(1 for r in rows if r.status == IPStatus.free),
        "observed_ips": sum(1 for r in rows if r.status == IPStatus.observed),
        "conflicts": sum(1 for r in rows if r.conflict),
    }


def next_free_ip(db: Session, subnet: Subnet) -> str | None:
    rows = subnet_rows(db, subnet)
    for r in rows:
        if r.status == IPStatus.free:
            return r.ip_address
    return None


def unknown_devices(db: Session) -> list[dict]:
    """Observations whose IP/MAC aren't represented in the Device/IPAddress tables."""
    obs_rows = db.execute(select(Observation)).scalars().all()
    subnets = db.execute(select(Subnet)).scalars().all()

    known_macs = set()
    from app.models import Device, IgnoredHost

    for d in db.execute(select(Device)).scalars().all():
        if d.mac_address:
            known_macs.add(d.mac_address)

    ignored_macs: set[str] = set()
    ignored_ips: set[str] = set()
    for ih in db.execute(select(IgnoredHost)).scalars().all():
        if ih.mac_address:
            ignored_macs.add(ih.mac_address)
        if ih.ip_address:
            ignored_ips.add(ih.ip_address)

    latest: dict[str, Observation] = {}
    latest_mac: dict[str, Observation] = {}
    for o in obs_rows:
        cur = latest.get(o.ip_address)
        if cur is None or o.last_seen > cur.last_seen:
            latest[o.ip_address] = o
        if o.mac_address:
            cur_mac = latest_mac.get(o.ip_address)
            if cur_mac is None or o.last_seen > cur_mac.last_seen:
                latest_mac[o.ip_address] = o

    out: list[dict] = []
    for ip, o in latest.items():
        # Use the best-known MAC for this IP (from any ARP observation), so
        # that a later ping scan doesn't make a known device look "unknown".
        mac_obs = latest_mac.get(ip)
        mac = mac_obs.mac_address if mac_obs else o.mac_address

        if mac and mac in known_macs:
            continue
        if mac and mac in ignored_macs:
            continue
        if ip in ignored_ips:
            continue
        subnet_id = None
        for s in subnets:
            if ip_in_subnet(ip, s.cidr):
                subnet_id = s.id
                break
        out.append(
            {
                "ip_address": ip,
                "mac_address": mac,
                "vendor": lookup_vendor(mac) if mac else None,
                "hostname": o.hostname,
                "source": o.source,
                "first_seen": o.first_seen,
                "last_seen": o.last_seen,
                "subnet_id": subnet_id,
            }
        )
    return out
