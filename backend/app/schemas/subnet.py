from datetime import datetime

from pydantic import BaseModel, ConfigDict, Field, field_validator

from app.utils.netaddr import validate_cidr, validate_ip


class SubnetCreate(BaseModel):
    name: str = Field(min_length=1, max_length=128)
    cidr: str
    gateway: str | None = None
    vlan: int | None = Field(default=None, ge=0, le=4094)
    description: str | None = None

    @field_validator("cidr")
    @classmethod
    def _check_cidr(cls, v: str) -> str:
        return validate_cidr(v)

    @field_validator("gateway")
    @classmethod
    def _check_gateway(cls, v: str | None) -> str | None:
        if v is None or v == "":
            return None
        return validate_ip(v)


class SubnetUpdate(BaseModel):
    name: str | None = None
    gateway: str | None = None
    vlan: int | None = None
    description: str | None = None
    scan_enabled: bool | None = None
    scan_interval_minutes: int | None = Field(default=None, ge=1, le=1440)
    scan_method: str | None = Field(default=None, pattern="^(arp|ping|nmap)$")


class SubnetOut(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: int
    name: str
    cidr: str
    gateway: str | None
    vlan: int | None
    description: str | None
    created_at: datetime
    scan_enabled: bool = False
    scan_interval_minutes: int | None = None
    scan_method: str = "arp"
    last_scanned_at: datetime | None = None


class SubnetWithStats(SubnetOut):
    total_ips: int
    assigned_ips: int
    free_ips: int
    observed_ips: int
    conflicts: int
