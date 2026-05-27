from fastapi import APIRouter, Depends, HTTPException, UploadFile, status
from sqlalchemy import select
from sqlalchemy.orm import Session

from app.auth import get_current_user
from app.database import get_db
from app.models import (
    Device,
    IgnoredHost,
    IPAddress,
    IPStatus,
    Observation,
    ObservationSource,
    Subnet,
    User,
)
from app.models.device import DeviceType
from app.scanners import (
    parse_dnsmasq_leases,
    parse_isc_leases,
    parse_lease_csv,
    parse_lease_json,
    parse_wg,
)
from app.scanners.pihole import (
    PiholeApiError,
    PiholeAuthError,
    PiholeConfig,
    fetch_local_dns,
    fetch_network_devices,
)
from app.services.scanjobs import job_manager
from app.services.scanning import run_scan
from app.utils.netaddr import ip_in_subnet
from app.utils.oui import lookup_vendor
from app.scanners.rdns import resolve_hostname
from app.schemas import (
    IgnoredHostOut,
    IgnoreHostRequest,
    LeaseImportItem,
    ObservationOut,
    PiholeImportRequest,
    PiholeImportResult,
    ProbeRequest,
    ProbeResult,
    ScanRequest,
    UnknownDeviceOut,
)
from app.services.observations import record_observation
from app.services.reconcile import unknown_devices

router = APIRouter(prefix="/discovery", tags=["discovery"])


@router.post("/scan", status_code=status.HTTP_202_ACCEPTED)
def trigger_scan(
    req: ScanRequest,
    db: Session = Depends(get_db),
    _: User = Depends(get_current_user),
) -> dict:
    """Run a scan synchronously and return the result."""
    subnet = db.get(Subnet, req.subnet_id)
    if subnet is None:
        raise HTTPException(status_code=404, detail="Subnet not found")
    found = run_scan(db, subnet, req.method, req.timeout)
    return {"subnet_id": subnet.id, "method": req.method, "observations": found}


@router.post("/scan/async", status_code=status.HTTP_202_ACCEPTED)
def trigger_scan_async(
    req: ScanRequest,
    db: Session = Depends(get_db),
    _: User = Depends(get_current_user),
) -> dict:
    """Queue a scan as a background job and return the job descriptor."""
    subnet = db.get(Subnet, req.subnet_id)
    if subnet is None:
        raise HTTPException(status_code=404, detail="Subnet not found")
    job = job_manager.submit(subnet.id, req.method, req.timeout, trigger="manual")
    return job.to_dict()


@router.get("/jobs")
def list_scan_jobs(_: User = Depends(get_current_user)) -> list[dict]:
    return [j.to_dict() for j in job_manager.list()]


@router.get("/jobs/{job_id}")
def get_scan_job(job_id: str, _: User = Depends(get_current_user)) -> dict:
    job = job_manager.get(job_id)
    if job is None:
        raise HTTPException(status_code=404, detail="Job not found")
    return job.to_dict()


@router.post("/probe", response_model=ProbeResult)
def probe_ip(
    req: ProbeRequest,
    db: Session = Depends(get_db),
    _: User = Depends(get_current_user),
) -> ProbeResult:
    """Probe a single IP address via ARP or ping, record the observation, and return the result."""
    cidr = f"{req.ip_address}/32"
    mac_address: str | None = None
    reachable = False

    if req.method == "arp":
        from app.scanners.arp import arp_scan
        hits = arp_scan(cidr, timeout=req.timeout)
        if hits:
            reachable = True
            mac_address = hits[0].mac_address
    else:
        from app.scanners.ping import ping_sweep
        hits = ping_sweep(cidr, timeout=req.timeout)
        if hits:
            reachable = True

    hostname: str | None = None
    if reachable:
        hostname = resolve_hostname(req.ip_address)
        source = ObservationSource.arp if req.method == "arp" else ObservationSource.ping
        record_observation(db, ip_address=req.ip_address, mac_address=mac_address,
                           hostname=hostname, source=source)
        db.commit()

    return ProbeResult(
        ip_address=req.ip_address,
        reachable=reachable,
        mac_address=mac_address,
        mac_vendor=lookup_vendor(mac_address) if mac_address else None,
        hostname=hostname,
    )


@router.get("/observations", response_model=list[ObservationOut])
def list_observations(
    limit: int = 500,
    db: Session = Depends(get_db),
    _: User = Depends(get_current_user),
) -> list[dict]:
    stmt = select(Observation).order_by(Observation.last_seen.desc()).limit(limit)
    rows = db.execute(stmt).scalars().all()
    return [
        {
            "id": o.id,
            "ip_address": o.ip_address,
            "mac_address": o.mac_address,
            "hostname": o.hostname,
            "vendor": lookup_vendor(o.mac_address),
            "source": o.source,
            "first_seen": o.first_seen,
            "last_seen": o.last_seen,
        }
        for o in rows
    ]


@router.get("/unknown", response_model=list[UnknownDeviceOut])
def list_unknown_devices(
    db: Session = Depends(get_db),
    _: User = Depends(get_current_user),
) -> list[dict]:
    return unknown_devices(db)


@router.post("/ignore", response_model=IgnoredHostOut, status_code=status.HTTP_201_CREATED)
def ignore_host(
    req: IgnoreHostRequest,
    db: Session = Depends(get_db),
    _: User = Depends(get_current_user),
) -> IgnoredHost:
    """Dismiss a host from the unknown list, matched by MAC and/or IP."""
    if not req.mac_address and not req.ip_address:
        raise HTTPException(status_code=400, detail="Provide a MAC and/or IP to ignore")
    ih = IgnoredHost(mac_address=req.mac_address, ip_address=req.ip_address, note=req.note)
    db.add(ih)
    db.commit()
    db.refresh(ih)
    return ih


@router.get("/ignored", response_model=list[IgnoredHostOut])
def list_ignored_hosts(
    db: Session = Depends(get_db),
    _: User = Depends(get_current_user),
) -> list[IgnoredHost]:
    return db.execute(
        select(IgnoredHost).order_by(IgnoredHost.created_at.desc())
    ).scalars().all()


@router.delete("/ignored/{ignored_id}", status_code=status.HTTP_204_NO_CONTENT)
def unignore_host(
    ignored_id: int,
    db: Session = Depends(get_db),
    _: User = Depends(get_current_user),
) -> None:
    ih = db.get(IgnoredHost, ignored_id)
    if ih is None:
        raise HTTPException(status_code=404, detail="Ignored host not found")
    db.delete(ih)
    db.commit()


@router.post("/leases/import")
def import_leases(
    items: list[LeaseImportItem],
    db: Session = Depends(get_db),
    _: User = Depends(get_current_user),
) -> dict:
    count = 0
    for item in items:
        record_observation(
            db,
            ip_address=item.ip_address,
            mac_address=item.mac_address,
            hostname=item.hostname,
            source=item.source,
        )
        count += 1
    db.commit()
    return {"imported": count}


@router.post("/leases/upload")
async def upload_leases(
    file: UploadFile,
    format: str = "auto",
    db: Session = Depends(get_db),
    _: User = Depends(get_current_user),
) -> dict:
    raw = (await file.read()).decode("utf-8", errors="replace")
    name = (file.filename or "").lower()
    fmt = format
    if fmt == "auto":
        if name.endswith(".json") or raw.lstrip().startswith(("{", "[")):
            fmt = "json"
        elif name.endswith(".csv"):
            fmt = "csv"
        elif "lease " in raw and "hardware ethernet" in raw:
            fmt = "isc"
        else:
            fmt = "dnsmasq"

    if fmt == "dnsmasq":
        leases = parse_dnsmasq_leases(raw)
    elif fmt == "isc":
        leases = parse_isc_leases(raw)
    elif fmt == "csv":
        leases = parse_lease_csv(raw)
    elif fmt == "json":
        leases = parse_lease_json(raw)
    else:
        raise HTTPException(status_code=400, detail=f"Unknown lease format: {fmt}")

    count = 0
    for lease in leases:
        record_observation(
            db,
            ip_address=lease.ip_address,
            mac_address=lease.mac_address,
            hostname=lease.hostname,
            source=ObservationSource.dhcp,
        )
        count += 1
    db.commit()
    return {"format": fmt, "imported": count}


@router.post("/wg/import")
async def import_wg_peers(
    file: UploadFile,
    subnet_id: int | None = None,
    db: Session = Depends(get_db),
    _: User = Depends(get_current_user),
) -> dict:
    """Import WireGuard peers from a wg0.conf or `wg show` output file.

    Creates authoritative device records (deduped by wg_pubkey) and assigns
    each peer's first IPv4 address as a static IP in the matching subnet.
    Unlike ARP/DHCP import, this creates *authoritative* assignments because
    WireGuard IPs are intentional allocations, not observations.
    """
    raw = (await file.read()).decode("utf-8", errors="replace")
    peers = parse_wg(raw)

    if not peers:
        raise HTTPException(status_code=400, detail="No WireGuard peers found in file")

    subnets = db.execute(select(Subnet)).scalars().all()
    if subnet_id is not None:
        chosen_subnet = db.get(Subnet, subnet_id)
        if chosen_subnet is None:
            raise HTTPException(status_code=404, detail="Subnet not found")

    created_devices = 0
    updated_devices = 0
    assigned_ips = 0
    skipped = 0

    for peer in peers:
        ip = _first_ipv4_from_peer(peer)

        # Find or create device by public key
        existing = db.execute(
            select(Device).where(Device.wg_pubkey == peer.pubkey)
        ).scalar_one_or_none()

        if existing is None:
            hostname = peer.name or f"wg-peer-{peer.pubkey[:8]}"
            device = Device(
                hostname=hostname,
                device_type=DeviceType.unknown,
                wg_pubkey=peer.pubkey,
            )
            db.add(device)
            db.flush()
            created_devices += 1
        else:
            device = existing
            if peer.name and device.hostname.startswith("wg-peer-"):
                device.hostname = peer.name
            updated_devices += 1

        if ip is None:
            skipped += 1
            continue

        # Find which subnet this IP belongs to (prefer explicit subnet_id)
        target_subnet: Subnet | None = None
        if subnet_id is not None and chosen_subnet is not None:
            if ip_in_subnet(ip, chosen_subnet.cidr):
                target_subnet = chosen_subnet
        if target_subnet is None:
            for s in subnets:
                if ip_in_subnet(ip, s.cidr):
                    target_subnet = s
                    break

        if target_subnet is None:
            skipped += 1
            continue

        # Create or update IP assignment (authoritative — static)
        existing_ip = db.execute(
            select(IPAddress).where(
                IPAddress.subnet_id == target_subnet.id,
                IPAddress.ip_address == ip,
            )
        ).scalar_one_or_none()

        if existing_ip is None:
            db.add(IPAddress(
                ip_address=ip,
                subnet_id=target_subnet.id,
                device_id=device.id,
                status=IPStatus.static,
                description=f"WireGuard peer {peer.pubkey[:12]}…",
            ))
            assigned_ips += 1
        elif existing_ip.device_id is None:
            existing_ip.device_id = device.id
            assigned_ips += 1

    db.commit()
    return {
        "peers_found": len(peers),
        "devices_created": created_devices,
        "devices_updated": updated_devices,
        "ips_assigned": assigned_ips,
        "skipped": skipped,
    }


@router.post("/pihole/import", response_model=PiholeImportResult)
def import_from_pihole(
    req: PiholeImportRequest,
    db: Session = Depends(get_db),
    _: User = Depends(get_current_user),
) -> PiholeImportResult:
    """Pull DHCP leases and local DNS records from a Pi-hole v6 instance."""
    config = PiholeConfig(url=req.url.rstrip("/"), password=req.password)
    result = PiholeImportResult()

    try:
        if req.import_leases:
            leases = fetch_network_devices(config)
            for lease in leases:
                try:
                    record_observation(
                        db,
                        ip_address=lease.ip_address,
                        mac_address=lease.mac_address,
                        hostname=lease.hostname,
                        source=ObservationSource.dhcp,
                    )
                    result.devices_imported += 1
                except Exception as exc:
                    result.errors.append(f"lease {lease.ip_address}: {exc}")

        if req.import_dns:
            dns_records = fetch_local_dns(config)
            for rec in dns_records:
                try:
                    record_observation(
                        db,
                        ip_address=rec.ip,
                        mac_address=None,
                        hostname=rec.hostname,
                        source=ObservationSource.dhcp,
                    )
                    result.dns_records_imported += 1
                except Exception as exc:
                    result.errors.append(f"dns {rec.ip}: {exc}")

    except PiholeAuthError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
    except PiholeApiError as exc:
        raise HTTPException(status_code=502, detail=str(exc)) from exc

    db.commit()
    return result


def _first_ipv4_from_peer(peer) -> str | None:
    import ipaddress
    for entry in peer.allowed_ips:
        ip_str = entry.split("/")[0].strip()
        try:
            addr = ipaddress.ip_address(ip_str)
            if isinstance(addr, ipaddress.IPv4Address):
                return str(addr)
        except ValueError:
            continue
    return None
