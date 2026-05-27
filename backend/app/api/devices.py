import csv
import json
from io import StringIO

from fastapi import APIRouter, Depends, HTTPException, UploadFile, status
from sqlalchemy import select
from sqlalchemy.orm import Session

from app.auth import get_current_user
from app.database import get_db
from app.models import Device, IPAddress, Observation, Subnet, User
from app.models.device import DeviceType
from app.schemas import DeviceCreate, DeviceDetail, DeviceOut, DeviceUpdate
from app.utils.netaddr import ip_in_subnet, normalize_mac

router = APIRouter(prefix="/devices", tags=["devices"])


def _latest_obs_by_mac(db: Session) -> dict[str, Observation]:
    """Map each MAC to its most recently seen observation."""
    latest: dict[str, Observation] = {}
    for o in db.execute(select(Observation).where(Observation.mac_address.isnot(None))).scalars():
        if o.mac_address is None:
            continue
        cur = latest.get(o.mac_address)
        if cur is None or o.last_seen > cur.last_seen:
            latest[o.mac_address] = o
    return latest


def _latest_obs_by_ip(db: Session) -> dict[str, Observation]:
    """Map each IP to its most recently seen observation (regardless of source)."""
    latest: dict[str, Observation] = {}
    for o in db.execute(select(Observation)).scalars():
        cur = latest.get(o.ip_address)
        if cur is None or o.last_seen > cur.last_seen:
            latest[o.ip_address] = o
    return latest


def _enrich(
    device: Device,
    obs_by_mac: dict[str, Observation],
    obs_by_ip: dict[str, Observation],
) -> Device:
    """Attach transient last_seen / primary_ip.

    primary_ip is taken from the latest MAC-matched observation (authoritative).
    last_seen then picks the more recent of that obs vs. any observation of the
    same IP — so ping scans (which lack a MAC) still keep the device "alive".
    """
    mac_obs = obs_by_mac.get(device.mac_address) if device.mac_address else None
    if mac_obs is None:
        device.last_seen = None
        device.primary_ip = None
        return device

    device.primary_ip = mac_obs.ip_address
    ip_obs = obs_by_ip.get(mac_obs.ip_address)
    if ip_obs is not None and ip_obs.last_seen > mac_obs.last_seen:
        device.last_seen = ip_obs.last_seen
    else:
        device.last_seen = mac_obs.last_seen
    return device


@router.get("", response_model=list[DeviceOut])
def list_devices(
    db: Session = Depends(get_db),
    _: User = Depends(get_current_user),
) -> list[Device]:
    devices = db.execute(select(Device).order_by(Device.hostname)).scalars().all()
    obs_by_mac = _latest_obs_by_mac(db)
    obs_by_ip = _latest_obs_by_ip(db)
    return [_enrich(d, obs_by_mac, obs_by_ip) for d in devices]


@router.post("", response_model=DeviceOut, status_code=status.HTTP_201_CREATED)
def create_device(
    payload: DeviceCreate,
    db: Session = Depends(get_db),
    _: User = Depends(get_current_user),
) -> Device:
    data = payload.model_dump()
    # Auto-fill vendor from the MAC OUI when the caller didn't provide one.
    if not data.get("vendor") and data.get("mac_address"):
        from app.utils.oui import lookup_vendor

        data["vendor"] = lookup_vendor(data["mac_address"])
    d = Device(**data)
    db.add(d)
    db.commit()
    db.refresh(d)
    return d


@router.get("/{device_id}", response_model=DeviceDetail)
def get_device(
    device_id: int,
    db: Session = Depends(get_db),
    _: User = Depends(get_current_user),
) -> Device:
    d = db.get(Device, device_id)
    if d is None:
        raise HTTPException(status_code=404, detail="Device not found")
    return _enrich(d, _latest_obs_by_mac(db), _latest_obs_by_ip(db))


@router.patch("/{device_id}", response_model=DeviceOut)
def update_device(
    device_id: int,
    payload: DeviceUpdate,
    db: Session = Depends(get_db),
    _: User = Depends(get_current_user),
) -> Device:
    d = db.get(Device, device_id)
    if d is None:
        raise HTTPException(status_code=404, detail="Device not found")
    for k, v in payload.model_dump(exclude_unset=True).items():
        setattr(d, k, v)
    db.commit()
    db.refresh(d)
    return d


@router.delete("/{device_id}", status_code=status.HTTP_204_NO_CONTENT)
def delete_device(
    device_id: int,
    db: Session = Depends(get_db),
    _: User = Depends(get_current_user),
) -> None:
    d = db.get(Device, device_id)
    if d is None:
        raise HTTPException(status_code=404, detail="Device not found")
    db.delete(d)
    db.commit()


@router.get("/{device_id}/ips")
def device_ips(
    device_id: int,
    db: Session = Depends(get_db),
    _: User = Depends(get_current_user),
) -> list[dict]:
    """Return all IP assignments for a device, merged with observation data."""
    if db.get(Device, device_id) is None:
        raise HTTPException(status_code=404, detail="Device not found")

    ips = db.execute(
        select(IPAddress).where(IPAddress.device_id == device_id)
    ).scalars().all()

    subnets = db.execute(select(Subnet)).scalars().all()
    subnet_map = {s.id: s for s in subnets}

    obs_by_ip: dict[str, Observation] = {}
    if ips:
        ip_addrs = [ip.ip_address for ip in ips]
        for o in db.execute(
            select(Observation).where(Observation.ip_address.in_(ip_addrs))
        ).scalars().all():
            cur = obs_by_ip.get(o.ip_address)
            if cur is None or o.last_seen > cur.last_seen:
                obs_by_ip[o.ip_address] = o

    out = []
    for ip in ips:
        obs = obs_by_ip.get(ip.ip_address)
        subnet = subnet_map.get(ip.subnet_id)
        ts = (obs.last_seen if obs else ip.last_seen)
        out.append({
            "ip_address": ip.ip_address,
            "subnet_id": ip.subnet_id,
            "subnet_name": subnet.name if subnet else None,
            "subnet_cidr": subnet.cidr if subnet else None,
            "status": ip.status.value,
            "assignment_id": ip.id,
            "description": ip.description,
            "observed_mac": obs.mac_address if obs else None,
            "observed_hostname": obs.hostname if obs else None,
            "last_seen": ts.isoformat() if ts else None,
        })
    return out


@router.post("/import", status_code=status.HTTP_200_OK)
async def import_devices(
    file: UploadFile,
    db: Session = Depends(get_db),
    _: User = Depends(get_current_user),
) -> dict:
    """Import devices from CSV or JSON.

    CSV expected columns: hostname, mac_address, vendor, device_type, notes (all optional except hostname).
    JSON: array of objects with the same fields.

    Existing devices matched by MAC address are skipped (no overwrite).
    """
    raw = (await file.read()).decode("utf-8", errors="replace")
    name = (file.filename or "").lower()

    rows: list[dict] = []
    if name.endswith(".json") or raw.lstrip().startswith(("[", "{")):
        data = json.loads(raw)
        if isinstance(data, dict):
            data = data.get("devices", data.get("data", []))
        rows = [r for r in data if isinstance(r, dict)]
    else:
        reader = csv.DictReader(StringIO(raw))
        rows = list(reader)

    created = 0
    skipped = 0
    errors: list[str] = []

    for i, row in enumerate(rows):
        hostname = (row.get("hostname") or row.get("name") or "").strip()
        if not hostname:
            errors.append(f"row {i + 1}: missing hostname")
            continue

        mac_raw = (row.get("mac_address") or row.get("mac") or "").strip()
        mac: str | None = None
        if mac_raw:
            try:
                mac = normalize_mac(mac_raw)
            except ValueError:
                errors.append(f"row {i + 1}: invalid MAC {mac_raw!r}")
                continue

        # Skip if MAC already known
        if mac:
            existing = db.execute(
                select(Device).where(Device.mac_address == mac)
            ).scalar_one_or_none()
            if existing is not None:
                skipped += 1
                continue

        dt_raw = (row.get("device_type") or row.get("type") or "unknown").strip()
        try:
            dt = DeviceType(dt_raw)
        except ValueError:
            dt = DeviceType.unknown

        db.add(
            Device(
                hostname=hostname,
                mac_address=mac,
                vendor=(row.get("vendor") or "").strip() or None,
                model=(row.get("model") or "").strip() or None,
                location=(row.get("location") or "").strip() or None,
                device_type=dt,
                notes=(row.get("notes") or "").strip() or None,
            )
        )
        created += 1

    db.commit()
    return {"created": created, "skipped": skipped, "errors": errors}
