from fastapi import APIRouter, Depends, HTTPException, status
from sqlalchemy import select
from sqlalchemy.orm import Session

from app.auth import get_current_user
from app.database import get_db
from app.models import IPAddress, IPStatus, Subnet, User
from app.schemas import IPAddressCreate, IPAddressOut, IPAddressUpdate
from app.utils.netaddr import ip_in_subnet

router = APIRouter(prefix="/ips", tags=["ips"])


@router.post("", response_model=IPAddressOut, status_code=status.HTTP_201_CREATED)
def create_ip(
    payload: IPAddressCreate,
    db: Session = Depends(get_db),
    _: User = Depends(get_current_user),
) -> IPAddress:
    subnet = db.get(Subnet, payload.subnet_id)
    if subnet is None:
        raise HTTPException(status_code=404, detail="Subnet not found")
    if not ip_in_subnet(payload.ip_address, subnet.cidr):
        raise HTTPException(status_code=400, detail="IP not in subnet")

    existing = db.execute(
        select(IPAddress).where(
            IPAddress.subnet_id == subnet.id, IPAddress.ip_address == payload.ip_address
        )
    ).scalar_one_or_none()
    if existing is not None:
        raise HTTPException(status_code=409, detail="IP already assigned in this subnet")

    ip = IPAddress(**payload.model_dump())
    db.add(ip)
    db.commit()
    db.refresh(ip)
    return ip


@router.patch("/{ip_id}", response_model=IPAddressOut)
def update_ip(
    ip_id: int,
    payload: IPAddressUpdate,
    db: Session = Depends(get_db),
    _: User = Depends(get_current_user),
) -> IPAddress:
    ip = db.get(IPAddress, ip_id)
    if ip is None:
        raise HTTPException(status_code=404, detail="IP not found")
    for k, v in payload.model_dump(exclude_unset=True).items():
        setattr(ip, k, v)
    db.commit()
    db.refresh(ip)
    return ip


@router.delete("/{ip_id}", status_code=status.HTTP_204_NO_CONTENT)
def release_ip(
    ip_id: int,
    db: Session = Depends(get_db),
    _: User = Depends(get_current_user),
) -> None:
    """Release an IP — deletes the assignment, making the IP free again."""
    ip = db.get(IPAddress, ip_id)
    if ip is None:
        raise HTTPException(status_code=404, detail="IP not found")
    db.delete(ip)
    db.commit()


@router.post("/reserve", response_model=IPAddressOut, status_code=status.HTTP_201_CREATED)
def reserve_ip(
    payload: IPAddressCreate,
    db: Session = Depends(get_db),
    _: User = Depends(get_current_user),
) -> IPAddress:
    """Reserve an IP with status=reserved."""
    subnet = db.get(Subnet, payload.subnet_id)
    if subnet is None:
        raise HTTPException(status_code=404, detail="Subnet not found")
    if not ip_in_subnet(payload.ip_address, subnet.cidr):
        raise HTTPException(status_code=400, detail="IP not in subnet")
    existing = db.execute(
        select(IPAddress).where(
            IPAddress.subnet_id == subnet.id, IPAddress.ip_address == payload.ip_address
        )
    ).scalar_one_or_none()
    if existing is not None:
        raise HTTPException(status_code=409, detail="IP already assigned in this subnet")
    ip = IPAddress(**{**payload.model_dump(), "status": IPStatus.reserved})
    db.add(ip)
    db.commit()
    db.refresh(ip)
    return ip
