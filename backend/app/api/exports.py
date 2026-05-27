from fastapi import APIRouter, Depends, HTTPException, UploadFile
from sqlalchemy import select
from sqlalchemy.orm import Session

from app.auth import get_current_admin, get_current_user
from app.database import get_db
from app.models import Device, IPAddress, Observation, Subnet, User
from app.utils.netaddr import normalize_mac, validate_cidr, validate_ip

router = APIRouter(prefix="/backup", tags=["backup"])


@router.get("/export")
def export_all(
    include_users: bool = False,
    db: Session = Depends(get_db),
    _: User = Depends(get_current_user),
) -> dict:
    subnets = db.execute(select(Subnet)).scalars().all()
    devices = db.execute(select(Device)).scalars().all()
    ips = db.execute(select(IPAddress)).scalars().all()
    obs = db.execute(select(Observation)).scalars().all()

    data: dict = {
        "version": 1,
        "subnets": [
            {
                "id": s.id,
                "name": s.name,
                "cidr": s.cidr,
                "gateway": s.gateway,
                "vlan": s.vlan,
                "description": s.description,
            }
            for s in subnets
        ],
        "devices": [
            {
                "id": d.id,
                "hostname": d.hostname,
                "mac_address": d.mac_address,
                "vendor": d.vendor,
                "device_type": d.device_type.value,
                "location": d.location,
                "notes": d.notes,
            }
            for d in devices
        ],
        "ip_addresses": [
            {
                "ip_address": i.ip_address,
                "subnet_id": i.subnet_id,
                "device_id": i.device_id,
                "status": i.status.value,
                "description": i.description,
            }
            for i in ips
        ],
        "observations": [
            {
                "ip_address": o.ip_address,
                "mac_address": o.mac_address,
                "hostname": o.hostname,
                "source": o.source.value,
                "first_seen": o.first_seen.isoformat(),
                "last_seen": o.last_seen.isoformat(),
            }
            for o in obs
        ],
    }
    if include_users:
        users = db.execute(select(User)).scalars().all()
        data["users"] = [
            {
                "username": u.username,
                "email": u.email,
                "is_admin": u.is_admin,
                # password_hash intentionally omitted unless explicitly requested
            }
            for u in users
        ]
    return data


@router.post("/import")
async def import_backup(
    file: UploadFile,
    db: Session = Depends(get_db),
    _: User = Depends(get_current_admin),
) -> dict:
    import json

    raw = (await file.read()).decode("utf-8", errors="replace")
    try:
        data = json.loads(raw)
    except json.JSONDecodeError as exc:
        raise HTTPException(status_code=400, detail=f"Invalid JSON: {exc}") from exc

    if not isinstance(data, dict):
        raise HTTPException(status_code=400, detail="Backup must be a JSON object")

    counts = {"subnets": 0, "devices": 0, "ip_addresses": 0, "observations": 0}

    subnet_id_map: dict[int, int] = {}
    for s in data.get("subnets", []):
        try:
            cidr = validate_cidr(s["cidr"])
        except (KeyError, ValueError):
            continue
        existing = db.execute(select(Subnet).where(Subnet.cidr == cidr)).scalar_one_or_none()
        if existing is None:
            existing = Subnet(
                name=s.get("name", cidr),
                cidr=cidr,
                gateway=s.get("gateway"),
                vlan=s.get("vlan"),
                description=s.get("description"),
            )
            db.add(existing)
            db.flush()
            counts["subnets"] += 1
        if "id" in s:
            subnet_id_map[int(s["id"])] = existing.id

    device_id_map: dict[int, int] = {}
    for d in data.get("devices", []):
        mac = d.get("mac_address")
        if mac:
            try:
                mac = normalize_mac(mac)
            except ValueError:
                mac = None
        existing = None
        if mac:
            existing = db.execute(
                select(Device).where(Device.mac_address == mac)
            ).scalar_one_or_none()
        if existing is None:
            from app.models.device import DeviceType

            dt_raw = d.get("device_type", "unknown")
            try:
                dt = DeviceType(dt_raw)
            except ValueError:
                dt = DeviceType.unknown
            existing = Device(
                hostname=d.get("hostname") or "unknown",
                mac_address=mac,
                vendor=d.get("vendor"),
                device_type=dt,
                location=d.get("location"),
                notes=d.get("notes"),
            )
            db.add(existing)
            db.flush()
            counts["devices"] += 1
        if "id" in d:
            device_id_map[int(d["id"])] = existing.id

    for i in data.get("ip_addresses", []):
        try:
            ip_addr = validate_ip(i["ip_address"])
            subnet_id = subnet_id_map.get(int(i["subnet_id"]), int(i["subnet_id"]))
        except (KeyError, ValueError):
            continue
        if db.get(Subnet, subnet_id) is None:
            continue
        if db.execute(
            select(IPAddress).where(
                IPAddress.subnet_id == subnet_id, IPAddress.ip_address == ip_addr
            )
        ).scalar_one_or_none():
            continue
        from app.models.ip_address import IPStatus

        try:
            status = IPStatus(i.get("status", "reserved"))
        except ValueError:
            status = IPStatus.reserved
        device_id = i.get("device_id")
        if device_id is not None:
            device_id = device_id_map.get(int(device_id), int(device_id))
        db.add(
            IPAddress(
                ip_address=ip_addr,
                subnet_id=subnet_id,
                device_id=device_id,
                status=status,
                description=i.get("description"),
            )
        )
        counts["ip_addresses"] += 1

    from datetime import datetime

    from app.models.observation import ObservationSource

    for o in data.get("observations", []):
        try:
            ip_addr = validate_ip(o["ip_address"])
        except (KeyError, ValueError):
            continue
        mac = o.get("mac_address")
        if mac:
            try:
                mac = normalize_mac(mac)
            except ValueError:
                mac = None
        try:
            src = ObservationSource(o.get("source", "arp"))
        except ValueError:
            src = ObservationSource.arp
        first_seen = datetime.fromisoformat(o["first_seen"]) if o.get("first_seen") else None
        last_seen = datetime.fromisoformat(o["last_seen"]) if o.get("last_seen") else None
        db.add(
            Observation(
                ip_address=ip_addr,
                mac_address=mac,
                hostname=o.get("hostname"),
                source=src,
                first_seen=first_seen or datetime.utcnow(),
                last_seen=last_seen or datetime.utcnow(),
            )
        )
        counts["observations"] += 1

    db.commit()
    return {"imported": counts}
