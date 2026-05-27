"""Periodic scan scheduler.

A single asyncio task wakes once a minute and submits background scan jobs for
any subnet whose configured interval has elapsed. Scans themselves run in the
job manager's thread pool, so this loop never blocks.
"""
from __future__ import annotations

import asyncio
import logging
from datetime import datetime, timedelta, timezone

from sqlalchemy import select

from app.database import SessionLocal
from app.models import Subnet
from app.services.scanjobs import job_manager

log = logging.getLogger(__name__)

_TICK_SECONDS = 60


def _aware(dt: datetime | None) -> datetime | None:
    if dt is None:
        return None
    return dt if dt.tzinfo else dt.replace(tzinfo=timezone.utc)


def _tick() -> None:
    db = SessionLocal()
    try:
        now = datetime.now(timezone.utc)
        subnets = db.execute(
            select(Subnet).where(Subnet.scan_enabled.is_(True))
        ).scalars().all()
        for s in subnets:
            interval = s.scan_interval_minutes or 0
            if interval <= 0:
                continue
            last = _aware(s.last_scanned_at)
            due = last is None or (now - last) >= timedelta(minutes=interval)
            if not due:
                continue
            s.last_scanned_at = now
            db.commit()
            job_manager.submit(s.id, s.scan_method or "arp", trigger="scheduled")
            log.info("scheduled scan queued for subnet %s (%s)", s.id, s.cidr)
    finally:
        db.close()


async def scheduler_loop() -> None:
    log.info("scan scheduler started")
    while True:
        try:
            await asyncio.to_thread(_tick)
        except Exception:  # noqa: BLE001 — keep the loop alive
            log.exception("scheduler tick failed")
        await asyncio.sleep(_TICK_SECONDS)
