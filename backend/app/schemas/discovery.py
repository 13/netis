from datetime import datetime

from pydantic import BaseModel, ConfigDict, Field, field_validator

from app.models.observation import ObservationSource
from app.utils.netaddr import normalize_mac, validate_ip


class ScanRequest(BaseModel):
    subnet_id: int
    method: str = Field(default="arp", pattern="^(arp|ping|nmap)$")
    timeout: float = Field(default=2.0, ge=0.1, le=120.0)


class IgnoreHostRequest(BaseModel):
    mac_address: str | None = None
    ip_address: str | None = None
    note: str | None = None

    @field_validator("mac_address")
    @classmethod
    def _norm_mac(cls, v: str | None) -> str | None:
        if not v:
            return None
        return normalize_mac(v)


class IgnoredHostOut(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: int
    mac_address: str | None
    ip_address: str | None
    note: str | None
    created_at: datetime


class ProbeRequest(BaseModel):
    ip_address: str
    method: str = Field(default="arp", pattern="^(arp|ping)$")
    timeout: float = Field(default=2.0, ge=0.1, le=30.0)

    @field_validator("ip_address")
    @classmethod
    def _check_ip(cls, v: str) -> str:
        return validate_ip(v)


class ProbeResult(BaseModel):
    ip_address: str
    reachable: bool
    mac_address: str | None = None
    mac_vendor: str | None = None
    hostname: str | None = None


class LeaseImportItem(BaseModel):
    ip_address: str
    mac_address: str
    hostname: str | None = None
    source: ObservationSource = ObservationSource.dhcp

    @field_validator("ip_address")
    @classmethod
    def _check_ip(cls, v: str) -> str:
        return validate_ip(v)

    @field_validator("mac_address")
    @classmethod
    def _norm_mac(cls, v: str) -> str:
        return normalize_mac(v)


class PiholeImportRequest(BaseModel):
    url: str
    password: str
    import_leases: bool = True
    import_dns: bool = True


class PiholeImportResult(BaseModel):
    devices_imported: int = 0
    dns_records_imported: int = 0
    errors: list[str] = Field(default_factory=list)
