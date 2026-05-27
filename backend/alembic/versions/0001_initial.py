"""initial schema

Revision ID: 0001
Revises:
Create Date: 2026-05-21

"""
from typing import Sequence, Union

import sqlalchemy as sa
from alembic import op

revision: str = "0001"
down_revision: Union[str, None] = None
branch_labels: Union[str, Sequence[str], None] = None
depends_on: Union[str, Sequence[str], None] = None


def upgrade() -> None:
    op.create_table(
        "users",
        sa.Column("id", sa.Integer(), primary_key=True),
        sa.Column("username", sa.String(64), nullable=False, unique=True),
        sa.Column("email", sa.String(255), nullable=False, unique=True),
        sa.Column("password_hash", sa.String(255), nullable=False),
        sa.Column("is_admin", sa.Boolean(), nullable=False, server_default=sa.false()),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
    )
    op.create_index("ix_users_username", "users", ["username"])
    op.create_index("ix_users_email", "users", ["email"])

    op.create_table(
        "subnets",
        sa.Column("id", sa.Integer(), primary_key=True),
        sa.Column("name", sa.String(128), nullable=False),
        sa.Column("cidr", sa.String(64), nullable=False, unique=True),
        sa.Column("gateway", sa.String(64), nullable=True),
        sa.Column("vlan", sa.Integer(), nullable=True),
        sa.Column("description", sa.String(512), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
    )
    op.create_index("ix_subnets_name", "subnets", ["name"])
    op.create_index("ix_subnets_cidr", "subnets", ["cidr"])

    device_type = sa.Enum(
        "router",
        "switch",
        "server",
        "vm",
        "container",
        "workstation",
        "iot",
        "unknown",
        name="device_type",
    )
    op.create_table(
        "devices",
        sa.Column("id", sa.Integer(), primary_key=True),
        sa.Column("hostname", sa.String(255), nullable=False),
        sa.Column("mac_address", sa.String(32), nullable=True),
        sa.Column("vendor", sa.String(128), nullable=True),
        sa.Column("device_type", device_type, nullable=False, server_default="unknown"),
        sa.Column("notes", sa.String(1024), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
    )
    op.create_index("ix_devices_hostname", "devices", ["hostname"])
    op.create_index("ix_devices_mac_address", "devices", ["mac_address"])

    ip_status = sa.Enum(
        "free", "reserved", "static", "dhcp", "observed", "conflict", name="ip_status"
    )
    op.create_table(
        "ip_addresses",
        sa.Column("id", sa.Integer(), primary_key=True),
        sa.Column("ip_address", sa.String(64), nullable=False),
        sa.Column(
            "subnet_id",
            sa.Integer(),
            sa.ForeignKey("subnets.id", ondelete="CASCADE"),
            nullable=False,
        ),
        sa.Column(
            "device_id",
            sa.Integer(),
            sa.ForeignKey("devices.id", ondelete="SET NULL"),
            nullable=True,
        ),
        sa.Column("status", ip_status, nullable=False, server_default="reserved"),
        sa.Column("last_seen", sa.DateTime(timezone=True), nullable=True),
        sa.Column("description", sa.String(512), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.UniqueConstraint("subnet_id", "ip_address", name="uq_subnet_ip"),
    )
    op.create_index("ix_ip_addresses_ip_address", "ip_addresses", ["ip_address"])
    op.create_index("ix_ip_addresses_subnet_id", "ip_addresses", ["subnet_id"])
    op.create_index("ix_ip_addresses_device_id", "ip_addresses", ["device_id"])
    op.create_index("ix_ip_addresses_status", "ip_addresses", ["status"])

    obs_source = sa.Enum("arp", "ping", "dhcp", name="observation_source")
    op.create_table(
        "observations",
        sa.Column("id", sa.Integer(), primary_key=True),
        sa.Column("ip_address", sa.String(64), nullable=False),
        sa.Column("mac_address", sa.String(32), nullable=True),
        sa.Column("hostname", sa.String(255), nullable=True),
        sa.Column("source", obs_source, nullable=False),
        sa.Column("first_seen", sa.DateTime(timezone=True), nullable=False),
        sa.Column("last_seen", sa.DateTime(timezone=True), nullable=False),
    )
    op.create_index("ix_observations_ip_address", "observations", ["ip_address"])
    op.create_index("ix_observations_mac_address", "observations", ["mac_address"])


def downgrade() -> None:
    op.drop_table("observations")
    op.drop_table("ip_addresses")
    op.drop_table("devices")
    op.drop_table("subnets")
    op.drop_table("users")
    sa.Enum(name="observation_source").drop(op.get_bind(), checkfirst=True)
    sa.Enum(name="ip_status").drop(op.get_bind(), checkfirst=True)
    sa.Enum(name="device_type").drop(op.get_bind(), checkfirst=True)
