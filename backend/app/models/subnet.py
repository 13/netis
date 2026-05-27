from datetime import datetime
from typing import TYPE_CHECKING

from sqlalchemy import Boolean, DateTime, Integer, String
from sqlalchemy.orm import Mapped, mapped_column, relationship

from app.database.base import Base, TimestampMixin

if TYPE_CHECKING:
    from .ip_address import IPAddress


class Subnet(Base, TimestampMixin):
    __tablename__ = "subnets"

    id: Mapped[int] = mapped_column(primary_key=True)
    name: Mapped[str] = mapped_column(String(128), nullable=False, index=True)
    cidr: Mapped[str] = mapped_column(String(64), unique=True, nullable=False, index=True)
    gateway: Mapped[str | None] = mapped_column(String(64), nullable=True)
    vlan: Mapped[int | None] = mapped_column(Integer, nullable=True)
    description: Mapped[str | None] = mapped_column(String(512), nullable=True)

    # Scheduled scanning
    scan_enabled: Mapped[bool] = mapped_column(Boolean, default=False, nullable=False)
    scan_interval_minutes: Mapped[int | None] = mapped_column(Integer, nullable=True)
    scan_method: Mapped[str] = mapped_column(String(8), default="arp", nullable=False)
    last_scanned_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)

    ip_addresses: Mapped[list["IPAddress"]] = relationship(
        back_populates="subnet", cascade="all, delete-orphan"
    )
