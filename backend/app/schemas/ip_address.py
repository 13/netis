from datetime import datetime

from pydantic import BaseModel, ConfigDict, field_validator

from app.models.ip_address import IPStatus
from app.utils.netaddr import validate_ip


class IPAddressCreate(BaseModel):
    ip_address: str
    subnet_id: int
    device_id: int | None = None
    status: IPStatus = IPStatus.reserved
    description: str | None = None

    @field_validator("ip_address")
    @classmethod
    def _check_ip(cls, v: str) -> str:
        return validate_ip(v)


class IPAddressUpdate(BaseModel):
    device_id: int | None = None
    status: IPStatus | None = None
    description: str | None = None


class IPAddressOut(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: int
    ip_address: str
    subnet_id: int
    device_id: int | None
    status: IPStatus
    last_seen: datetime | None
    description: str | None
