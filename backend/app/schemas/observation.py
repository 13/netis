from datetime import datetime

from pydantic import BaseModel, ConfigDict

from app.models.observation import ObservationSource


class ObservationOut(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: int
    ip_address: str
    mac_address: str | None
    hostname: str | None
    vendor: str | None = None
    source: ObservationSource
    first_seen: datetime
    last_seen: datetime


class UnknownDeviceOut(BaseModel):
    ip_address: str
    mac_address: str | None
    vendor: str | None = None
    hostname: str | None
    source: ObservationSource
    first_seen: datetime
    last_seen: datetime
    subnet_id: int | None
