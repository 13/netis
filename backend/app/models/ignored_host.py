from datetime import datetime

from sqlalchemy import DateTime, String
from sqlalchemy.orm import Mapped, mapped_column

from app.database.base import Base, utcnow


class IgnoredHost(Base):
    """A host (by MAC and/or IP) that the user has dismissed from the unknown list."""

    __tablename__ = "ignored_hosts"

    id: Mapped[int] = mapped_column(primary_key=True)
    mac_address: Mapped[str | None] = mapped_column(String(32), nullable=True, index=True)
    ip_address: Mapped[str | None] = mapped_column(String(64), nullable=True, index=True)
    note: Mapped[str | None] = mapped_column(String(512), nullable=True)
    created_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True), default=utcnow, nullable=False
    )
