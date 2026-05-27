import enum
from typing import TYPE_CHECKING

from sqlalchemy import Enum, ForeignKey, String
from sqlalchemy.orm import Mapped, mapped_column, relationship

from app.database.base import Base, TimestampMixin

if TYPE_CHECKING:
    from .ip_address import IPAddress


class DeviceType(str, enum.Enum):
    router = "router"
    switch = "switch"
    server = "server"
    vm = "vm"
    container = "container"
    workstation = "workstation"
    iot = "iot"
    unknown = "unknown"


class Device(Base, TimestampMixin):
    __tablename__ = "devices"

    id: Mapped[int] = mapped_column(primary_key=True)
    hostname: Mapped[str] = mapped_column(String(255), nullable=False, index=True)
    mac_address: Mapped[str | None] = mapped_column(String(32), nullable=True, index=True)
    vendor: Mapped[str | None] = mapped_column(String(128), nullable=True)
    device_type: Mapped[DeviceType] = mapped_column(
        Enum(DeviceType, name="device_type"), default=DeviceType.unknown, nullable=False
    )
    model: Mapped[str | None] = mapped_column(String(128), nullable=True)
    location: Mapped[str | None] = mapped_column(String(255), nullable=True)
    notes: Mapped[str | None] = mapped_column(String(1024), nullable=True)

    # WireGuard public key (base64-encoded, globally unique per peer)
    wg_pubkey: Mapped[str | None] = mapped_column(String(64), nullable=True, unique=True, index=True)

    # Hypervisor/host hierarchy: VMs and LXCs point to their Proxmox/ESXi host
    parent_device_id: Mapped[int | None] = mapped_column(
        ForeignKey("devices.id", ondelete="SET NULL"), nullable=True, index=True
    )
    children: Mapped[list["Device"]] = relationship(
        "Device",
        primaryjoin="Device.parent_device_id == Device.id",
        foreign_keys="Device.parent_device_id",
    )

    ip_addresses: Mapped[list["IPAddress"]] = relationship(back_populates="device")
