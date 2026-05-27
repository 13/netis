from sqlalchemy.orm import sessionmaker

from app.models import Subnet
from app.services.scanjobs import ScanJobManager
from app.utils.oui import lookup_vendor


def test_scan_job_runs_and_completes(db_engine):
    """A submitted job runs to completion against its own session."""
    SessionLocal = sessionmaker(bind=db_engine, autoflush=False, autocommit=False, future=True)

    # Seed a subnet in the shared in-memory engine.
    seed = SessionLocal()
    s = Subnet(name="lab", cidr="10.9.9.0/30")
    seed.add(s)
    seed.commit()
    subnet_id = s.id
    seed.close()

    mgr = ScanJobManager(session_factory=SessionLocal, max_workers=1)
    job = mgr.submit(subnet_id, method="ping", timeout=0.1)
    finished = mgr.wait(job.id, timeout=15)

    assert finished is not None
    # No raw-socket privileges in CI → scan yields nothing, but the job must
    # complete cleanly rather than error.
    assert finished.status == "done", finished.error
    assert finished.found >= 0


def test_scan_job_unknown_subnet_errors(db_engine):
    SessionLocal = sessionmaker(bind=db_engine, autoflush=False, autocommit=False, future=True)
    mgr = ScanJobManager(session_factory=SessionLocal, max_workers=1)
    job = mgr.submit(999999, method="ping", timeout=0.1)
    finished = mgr.wait(job.id, timeout=15)
    assert finished is not None
    assert finished.status == "error"
    assert "not found" in (finished.error or "").lower()


def test_oui_lookup_known_and_unknown():
    assert lookup_vendor("b8:27:eb:12:34:56") == "Raspberry Pi Foundation"
    assert lookup_vendor("00:0c:29:aa:bb:cc") == "VMware"
    assert lookup_vendor("ff:ff:ff:00:00:00") is None
    assert lookup_vendor(None) is None
    assert lookup_vendor("not-a-mac") is None
