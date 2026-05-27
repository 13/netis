"""Admin API — database configuration and migration."""

from __future__ import annotations

import os
import re
from typing import Literal

from fastapi import APIRouter, Depends, HTTPException, status
from pydantic import BaseModel
from sqlalchemy import create_engine, select, text
from sqlalchemy.orm import Session, sessionmaker

from app.auth import get_current_admin
from app.config import get_config_file_path, get_settings
from app.database import Base, get_db
from app.models import Device, IPAddress, Observation, Subnet, User

router = APIRouter(prefix="/admin", tags=["admin"])


# ── Local network detection ───────────────────────────────────────────────────


@router.get("/local-networks")
def local_networks(_: User = Depends(get_current_admin)) -> list[dict]:
    """Detect the host's private network interfaces for the setup wizard."""
    import ipaddress
    import json
    import subprocess

    results: list[dict] = []
    seen: set[str] = set()

    # Try `ip -j addr show` (Linux, iproute2)
    try:
        proc = subprocess.run(
            ["ip", "-j", "addr", "show"],
            capture_output=True, text=True, timeout=5
        )
        ifaces = json.loads(proc.stdout or "[]")
        for iface in ifaces:
            name: str = iface.get("ifname", "")
            if name == "lo" or iface.get("operstate") not in ("UP", "UNKNOWN"):
                continue
            for addr in iface.get("addr_info", []):
                if addr.get("family") != "inet":
                    continue
                ip_str = addr["local"]
                prefix = addr["prefixlen"]
                try:
                    net = ipaddress.ip_network(f"{ip_str}/{prefix}", strict=False)
                except ValueError:
                    continue
                if net.is_loopback or not net.is_private:
                    continue
                cidr = str(net)
                if cidr in seen:
                    continue
                seen.add(cidr)
                results.append({
                    "interface": name,
                    "ip": ip_str,
                    "cidr": cidr,
                    "name": name,
                    "gateway": ip_str,
                })
        if results:
            return results
    except Exception:
        pass

    # Fallback: Python socket (cross-platform, less detail)
    try:
        import socket
        import struct
        import fcntl
        import array

        SIOCGIFCONF = 0x8912
        buf = array.array("B", b"\0" * 1024)
        sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        ifc_len = struct.unpack("iL", fcntl.ioctl(sock.fileno(), SIOCGIFCONF, struct.pack("iL", 1024, buf.buffer_info()[0])))[0]
        sock.close()
        ifc_buf = buf.tobytes()
        for i in range(0, ifc_len, 40):
            ifname = ifc_buf[i:i+16].split(b"\0")[0].decode()
            raw_ip = ifc_buf[i+20:i+24]
            ip_str = socket.inet_ntoa(raw_ip)
            try:
                addr = ipaddress.ip_address(ip_str)
                if addr.is_loopback or not addr.is_private:
                    continue
                cidr = str(ipaddress.ip_network(f"{ip_str}/24", strict=False))
                if cidr not in seen:
                    seen.add(cidr)
                    results.append({"interface": ifname, "ip": ip_str, "cidr": cidr, "name": ifname, "gateway": ip_str})
            except Exception:
                continue
    except Exception:
        pass

    return results


# ── Helpers ───────────────────────────────────────────────────────────────────


def _redact_url(url: str) -> str:
    return re.sub(r":([^:/@]+)@", r":***@", url)


def _url_type(url: str) -> Literal["sqlite", "postgresql"]:
    if "postgresql" in url or "postgres" in url:
        return "postgresql"
    return "sqlite"


def _build_url(cfg: "DbConfig") -> str:
    if cfg.db_type == "sqlite":
        path = cfg.sqlite_path or "/data/netis.db"
        return f"sqlite:///{path}"
    if cfg.db_type == "postgresql":
        if not cfg.pg_host or not cfg.pg_user or not cfg.pg_database:
            raise HTTPException(status_code=400, detail="PostgreSQL requires host, user, and database")
        pw = f":{cfg.pg_password}@" if cfg.pg_password else "@"
        return f"postgresql+psycopg://{cfg.pg_user}{pw}{cfg.pg_host}:{cfg.pg_port}/{cfg.pg_database}"
    raise HTTPException(status_code=400, detail=f"Unknown db_type: {cfg.db_type!r}")


def _make_engine(url: str):
    connect_args: dict = {}
    if url.startswith("sqlite"):
        connect_args = {"check_same_thread": False}
    return create_engine(url, connect_args=connect_args, pool_pre_ping=True, future=True)


# ── Schemas ───────────────────────────────────────────────────────────────────


class DbConfig(BaseModel):
    db_type: Literal["sqlite", "postgresql"]
    sqlite_path: str | None = None
    pg_host: str | None = None
    pg_port: int = 5432
    pg_user: str | None = None
    pg_password: str | None = None
    pg_database: str | None = None


# ── Endpoints ─────────────────────────────────────────────────────────────────


@router.get("/info")
def admin_info(
    db: Session = Depends(get_db),
    _: User = Depends(get_current_admin),
) -> dict:
    """Return current database backend info."""
    settings = get_settings()
    url = settings.database_url
    db_type = _url_type(url)

    try:
        db.execute(text("SELECT 1"))
        connected = True
    except Exception:
        connected = False

    config_path = get_config_file_path()

    return {
        "db_type": db_type,
        "db_url_safe": _redact_url(url),
        "connected": connected,
        "config_file": config_path,
        "config_writable": _config_writable(config_path),
        "scheduler_enabled": settings.scheduler_enabled,
        "alert_webhook_configured": bool(settings.alert_webhook_url),
    }


def _config_writable(path: str) -> bool:
    try:
        dir_ = os.path.dirname(path) or "."
        return os.access(dir_, os.W_OK)
    except Exception:
        return False


@router.post("/test-db")
def test_db_connection(
    cfg: DbConfig,
    _: User = Depends(get_current_admin),
) -> dict:
    """Test a database connection without changing anything."""
    url = _build_url(cfg)
    try:
        eng = _make_engine(url)
        with eng.connect() as conn:
            conn.execute(text("SELECT 1"))
        eng.dispose()
        return {"ok": True, "url_safe": _redact_url(url)}
    except Exception as exc:
        return {"ok": False, "error": str(exc)}


@router.post("/migrate-db")
def migrate_db(
    cfg: DbConfig,
    db: Session = Depends(get_db),
    _: User = Depends(get_current_admin),
) -> dict:
    """
    Create schema on the target database, migrate all data from the current
    database, and write the new URL to the persistent config file.
    A backend restart is required for the switch to take effect.
    """
    target_url = _build_url(cfg)
    current_url = get_settings().database_url

    if target_url == current_url:
        raise HTTPException(status_code=400, detail="Target database is the same as the current one")

    # 1. Create schema on target
    try:
        target_engine = _make_engine(target_url)
        Base.metadata.create_all(bind=target_engine)
    except Exception as exc:
        raise HTTPException(status_code=502, detail=f"Cannot connect to target database: {exc}") from exc

    # 2. Export data from current DB
    subnets = db.execute(select(Subnet)).scalars().all()
    devices = db.execute(select(Device)).scalars().all()
    ips = db.execute(select(IPAddress)).scalars().all()
    obs = db.execute(select(Observation)).scalars().all()
    users = db.execute(select(User)).scalars().all()

    # 3. Import into target DB (additive, skip duplicates)
    TargetSession = sessionmaker(bind=target_engine, autoflush=False, autocommit=False, future=True)
    counts = {"subnets": 0, "devices": 0, "ip_addresses": 0, "observations": 0, "users": 0}

    with TargetSession() as tdb:
        subnet_id_map: dict[int, int] = {}
        for s in subnets:
            ex = tdb.execute(select(Subnet).where(Subnet.cidr == s.cidr)).scalar_one_or_none()
            if ex is None:
                new_s = Subnet(name=s.name, cidr=s.cidr, gateway=s.gateway, vlan=s.vlan, description=s.description)
                tdb.add(new_s)
                tdb.flush()
                subnet_id_map[s.id] = new_s.id
                counts["subnets"] += 1
            else:
                subnet_id_map[s.id] = ex.id

        device_id_map: dict[int, int] = {}
        for d in devices:
            ex = None
            if d.mac_address:
                ex = tdb.execute(select(Device).where(Device.mac_address == d.mac_address)).scalar_one_or_none()
            if ex is None:
                new_d = Device(
                    hostname=d.hostname, mac_address=d.mac_address,
                    vendor=d.vendor, device_type=d.device_type, notes=d.notes,
                )
                tdb.add(new_d)
                tdb.flush()
                device_id_map[d.id] = new_d.id
                counts["devices"] += 1
            else:
                device_id_map[d.id] = ex.id

        for ip in ips:
            sid = subnet_id_map.get(ip.subnet_id, ip.subnet_id)
            did = device_id_map.get(ip.device_id, ip.device_id) if ip.device_id else None
            ex = tdb.execute(
                select(IPAddress).where(IPAddress.subnet_id == sid, IPAddress.ip_address == ip.ip_address)
            ).scalar_one_or_none()
            if ex is None:
                tdb.add(IPAddress(ip_address=ip.ip_address, subnet_id=sid, device_id=did,
                                  status=ip.status, description=ip.description))
                counts["ip_addresses"] += 1

        for o in obs:
            ex = tdb.execute(
                select(Observation).where(
                    Observation.ip_address == o.ip_address, Observation.source == o.source
                )
            ).scalar_one_or_none()
            if ex is None:
                tdb.add(Observation(ip_address=o.ip_address, mac_address=o.mac_address,
                                    hostname=o.hostname, source=o.source,
                                    first_seen=o.first_seen, last_seen=o.last_seen))
                counts["observations"] += 1

        for u in users:
            ex = tdb.execute(select(User).where(User.username == u.username)).scalar_one_or_none()
            if ex is None:
                tdb.add(User(username=u.username, email=u.email,
                             password_hash=u.password_hash, is_admin=u.is_admin))
                counts["users"] += 1

        tdb.commit()

    target_engine.dispose()

    # 4. Write new URL to config file
    config_path = get_config_file_path()
    try:
        dir_ = os.path.dirname(config_path)
        if dir_:
            os.makedirs(dir_, exist_ok=True)
        with open(config_path, "w") as f:
            f.write(f"NETIS_DATABASE_URL={target_url}\n")
        saved = True
    except OSError as exc:
        saved = False

    return {
        "migrated": counts,
        "url_safe": _redact_url(target_url),
        "config_saved": saved,
        "restart_required": True,
    }
