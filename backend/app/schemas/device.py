from datetime import datetime

from pydantic import BaseModel, ConfigDict, Field, field_validator

from app.models.device import DeviceType
from app.utils.netaddr import normalize_mac


class DeviceCreate(BaseModel):
    hostname: str = Field(min_length=1, max_length=255)
    mac_address: str | None = None
    vendor: str | None = None
    model: str | None = None
    location: str | None = None
    device_type: DeviceType = DeviceType.unknown
    notes: str | None = None
    wg_pubkey: str | None = None
    parent_device_id: int | None = None

    @field_validator("mac_address")
    @classmethod
    def _norm_mac(cls, v: str | None) -> str | None:
        if v is None or v == "":
            return None
        return normalize_mac(v)


class DeviceUpdate(BaseModel):
    hostname: str | None = None
    mac_address: str | None = None
    vendor: str | None = None
    model: str | None = None
    location: str | None = None
    device_type: DeviceType | None = None
    notes: str | None = None
    wg_pubkey: str | None = None
    parent_device_id: int | None = None

    @field_validator("mac_address")
    @classmethod
    def _norm_mac(cls, v: str | None) -> str | None:
        if v is None or v == "":
            return None
        return normalize_mac(v)


class DeviceChild(BaseModel):
    """Shallow device summary used when listing children to avoid recursion."""
    model_config = ConfigDict(from_attributes=True)

    id: int
    hostname: str
    device_type: DeviceType
    mac_address: str | None
    wg_pubkey: str | None


class DeviceOut(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: int
    hostname: str
    mac_address: str | None
    vendor: str | None
    model: str | None
    location: str | None
    device_type: DeviceType
    notes: str | None
    wg_pubkey: str | None
    parent_device_id: int | None
    created_at: datetime
    updated_at: datetime
    # Enriched at the API layer from observations (latest match by MAC).
    last_seen: datetime | None = None
    primary_ip: str | None = None


class DeviceDetail(DeviceOut):
    """DeviceOut extended with eager-loaded children (for the GET /devices/{id} endpoint)."""
    children: list[DeviceChild] = []
