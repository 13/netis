from fastapi import APIRouter, Depends, HTTPException, status
from sqlalchemy import select
from sqlalchemy.orm import Session

from app.auth import get_current_user
from app.database import get_db
from app.models import Subnet, User
from app.schemas import SubnetCreate, SubnetOut, SubnetUpdate, SubnetWithStats
from app.services.reconcile import next_free_ip, subnet_rows, subnet_stats
from app.utils.oui import lookup_vendor

router = APIRouter(prefix="/subnets", tags=["subnets"])


@router.get("", response_model=list[SubnetWithStats])
def list_subnets(
    db: Session = Depends(get_db),
    _: User = Depends(get_current_user),
) -> list[SubnetWithStats]:
    out: list[SubnetWithStats] = []
    for s in db.execute(select(Subnet).order_by(Subnet.name)).scalars().all():
        stats = subnet_stats(db, s)
        out.append(SubnetWithStats(**SubnetOut.model_validate(s).model_dump(), **stats))
    return out


@router.post("", response_model=SubnetOut, status_code=status.HTTP_201_CREATED)
def create_subnet(
    payload: SubnetCreate,
    db: Session = Depends(get_db),
    _: User = Depends(get_current_user),
) -> Subnet:
    if db.execute(select(Subnet).where(Subnet.cidr == payload.cidr)).scalar_one_or_none():
        raise HTTPException(status_code=409, detail="Subnet with this CIDR already exists")
    s = Subnet(**payload.model_dump())
    db.add(s)
    db.commit()
    db.refresh(s)
    return s


@router.get("/{subnet_id}", response_model=SubnetWithStats)
def get_subnet(
    subnet_id: int,
    db: Session = Depends(get_db),
    _: User = Depends(get_current_user),
) -> SubnetWithStats:
    s = db.get(Subnet, subnet_id)
    if s is None:
        raise HTTPException(status_code=404, detail="Subnet not found")
    return SubnetWithStats(**SubnetOut.model_validate(s).model_dump(), **subnet_stats(db, s))


@router.patch("/{subnet_id}", response_model=SubnetOut)
def update_subnet(
    subnet_id: int,
    payload: SubnetUpdate,
    db: Session = Depends(get_db),
    _: User = Depends(get_current_user),
) -> Subnet:
    s = db.get(Subnet, subnet_id)
    if s is None:
        raise HTTPException(status_code=404, detail="Subnet not found")
    for k, v in payload.model_dump(exclude_unset=True).items():
        setattr(s, k, v)
    db.commit()
    db.refresh(s)
    return s


@router.delete("/{subnet_id}", status_code=status.HTTP_204_NO_CONTENT)
def delete_subnet(
    subnet_id: int,
    db: Session = Depends(get_db),
    _: User = Depends(get_current_user),
) -> None:
    s = db.get(Subnet, subnet_id)
    if s is None:
        raise HTTPException(status_code=404, detail="Subnet not found")
    db.delete(s)
    db.commit()


@router.get("/{subnet_id}/ips")
def list_subnet_ips(
    subnet_id: int,
    db: Session = Depends(get_db),
    _: User = Depends(get_current_user),
) -> list[dict]:
    s = db.get(Subnet, subnet_id)
    if s is None:
        raise HTTPException(status_code=404, detail="Subnet not found")
    rows = subnet_rows(db, s)
    return [
        {
            "ip_address": r.ip_address,
            "status": r.status.value,
            "assignment_id": r.assignment_id,
            "device_id": r.device_id,
            "description": r.description,
            "observed_mac": r.observed_mac,
            "observed_vendor": lookup_vendor(r.observed_mac) if r.observed_mac else None,
            "observed_hostname": r.observed_hostname,
            "last_seen": r.last_seen.isoformat() if r.last_seen else None,
            "conflict": r.conflict,
        }
        for r in rows
    ]


@router.get("/{subnet_id}/next-free")
def next_free(
    subnet_id: int,
    db: Session = Depends(get_db),
    _: User = Depends(get_current_user),
) -> dict:
    s = db.get(Subnet, subnet_id)
    if s is None:
        raise HTTPException(status_code=404, detail="Subnet not found")
    ip = next_free_ip(db, s)
    return {"ip_address": ip}
