"""In-process background scan jobs.

Scans use blocking I/O (scapy, subprocess), so they run in a thread pool rather
than on the event loop. Job state is held in memory — it is operational status,
not durable data, so it is fine to lose on restart.
"""
from __future__ import annotations

import logging
import threading
import uuid
from concurrent.futures import ThreadPoolExecutor
from dataclasses import asdict, dataclass
from datetime import datetime, timezone

from app.database import SessionLocal
from app.models import Subnet
from app.services.scanning import run_scan

log = logging.getLogger(__name__)


def _now() -> datetime:
    return datetime.now(timezone.utc)


@dataclass
class ScanJob:
    id: str
    subnet_id: int
    method: str
    trigger: str
    status: str  # queued | running | done | error
    created_at: datetime
    started_at: datetime | None = None
    finished_at: datetime | None = None
    found: int = 0
    error: str | None = None

    def to_dict(self) -> dict:
        d = asdict(self)
        for k in ("created_at", "started_at", "finished_at"):
            d[k] = d[k].isoformat() if d[k] else None
        return d


class ScanJobManager:
    def __init__(self, session_factory=SessionLocal, max_workers: int = 2):
        self._factory = session_factory
        self._pool = ThreadPoolExecutor(max_workers=max_workers, thread_name_prefix="scan")
        self._jobs: dict[str, ScanJob] = {}
        self._events: dict[str, threading.Event] = {}
        self._lock = threading.Lock()

    def submit(
        self, subnet_id: int, method: str = "arp", timeout: float = 2.0, trigger: str = "manual"
    ) -> ScanJob:
        job = ScanJob(
            id=uuid.uuid4().hex[:12],
            subnet_id=subnet_id,
            method=method,
            trigger=trigger,
            status="queued",
            created_at=_now(),
        )
        with self._lock:
            self._jobs[job.id] = job
            self._events[job.id] = threading.Event()
        self._pool.submit(self._run, job.id, method, timeout)
        return job

    def _run(self, job_id: str, method: str, timeout: float) -> None:
        job = self._jobs[job_id]
        job.status = "running"
        job.started_at = _now()
        db = self._factory()
        try:
            subnet = db.get(Subnet, job.subnet_id)
            if subnet is None:
                raise ValueError("Subnet not found")
            job.found = run_scan(db, subnet, method, timeout)
            job.status = "done"
        except Exception as exc:  # noqa: BLE001 — record failure on the job
            db.rollback()
            job.error = str(exc)
            job.status = "error"
            log.warning("scan job %s failed: %s", job_id, exc)
        finally:
            db.close()
            job.finished_at = _now()
            self._events[job_id].set()

    def get(self, job_id: str) -> ScanJob | None:
        return self._jobs.get(job_id)

    def list(self, limit: int = 50) -> list[ScanJob]:
        jobs = sorted(self._jobs.values(), key=lambda j: j.created_at, reverse=True)
        return jobs[:limit]

    def wait(self, job_id: str, timeout: float = 30.0) -> ScanJob | None:
        ev = self._events.get(job_id)
        if ev:
            ev.wait(timeout)
        return self._jobs.get(job_id)


# App-wide singleton (bound to the real SessionLocal).
job_manager = ScanJobManager()
