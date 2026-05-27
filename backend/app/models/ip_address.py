import enum
from datetime import datetime
from typing import TYPE_CHECKING

from sqlalchemy import DateTime, Enum, ForeignKey, String, UniqueConstraint
from sqlalchemy.orm import Mapped, mapped_column, relationship

from app.database.base import Base, TimestampMixin

if TYPE_CHECKING:
    from .device import Device
    from .subnet import Subnet


class IPStatus(str, enum.Enum):
    free = "free"
    reserved = "reserved"
    static = "static"
    dhcp = "dhcp"
    observed = "observed"
    conflict = "conflict"


class IPAddress(Base, TimestampMixin):
    __tablename__ = "ip_addresses"
    __table_args__ = (UniqueConstraint("subnet_id", "ip_address", name="uq_subnet_ip"),)

    id: Mapped[int] = mapped_column(primary_key=True)
    ip_address: Mapped[str] = mapped_column(String(64), nullable=False, index=True)
    subnet_id: Mapped[int] = mapped_column(
        ForeignKey("subnets.id", ondelete="CASCADE"), nullable=False, index=True
    )
    device_id: Mapped[int | None] = mapped_column(
        ForeignKey("devices.id", ondelete="SET NULL"), nullable=True, index=True
    )
    status: Mapped[IPStatus] = mapped_column(
        Enum(IPStatus, name="ip_status"), default=IPStatus.reserved, nullable=False, index=True
    )
    last_seen: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    description: Mapped[str | None] = mapped_column(String(512), nullable=True)

    subnet: Mapped["Subnet"] = relationship(back_populates="ip_addresses")
    device: Mapped["Device | None"] = relationship(back_populates="ip_addresses")
