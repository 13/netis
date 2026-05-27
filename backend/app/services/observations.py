"""Record observed network state. Never mutates authoritative assignments."""

from __future__ import annotations

from datetime import datetime, timezone

from sqlalchemy import select
from sqlalchemy.orm import Session

from app.models import IPAddress, Observation, ObservationSource


def record_observation(
    db: Session,
    *,
    ip_address: str,
    mac_address: str | None,
    hostname: str | None,
    source: ObservationSource,
) -> Observation:
    now = datetime.now(timezone.utc)
    stmt = select(Observation).where(
        Observation.ip_address == ip_address,
        Observation.mac_address == mac_address,
        Observation.source == source,
    )
    existing = db.execute(stmt).scalar_one_or_none()
    if existing is not None:
        existing.last_seen = now
        if hostname and not existing.hostname:
            existing.hostname = hostname
        db.add(existing)
        obs = existing
    else:
        obs = Observation(
            ip_address=ip_address,
            mac_address=mac_address,
            hostname=hostname,
            source=source,
            first_seen=now,
            last_seen=now,
        )
        db.add(obs)

    # Update last_seen on any matching IPAddress row (read-only touch — does not
    # change assignments). This keeps the subnet view's "last_seen" column fresh.
    ip_rows = db.execute(
        select(IPAddress).where(IPAddress.ip_address == ip_address)
    ).scalars().all()
    for ip in ip_rows:
        ip.last_seen = now

    return obs
