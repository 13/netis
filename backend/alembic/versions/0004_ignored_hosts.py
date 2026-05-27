"""add ignored_hosts table

Revision ID: 0004
Revises: 0003
Create Date: 2026-05-22
"""
from typing import Sequence, Union

import sqlalchemy as sa
from alembic import op

revision: str = "0004"
down_revision: Union[str, None] = "0003"
branch_labels: Union[str, Sequence[str], None] = None
depends_on: Union[str, Sequence[str], None] = None


def upgrade() -> None:
    op.create_table(
        "ignored_hosts",
        sa.Column("id", sa.Integer(), primary_key=True),
        sa.Column("mac_address", sa.String(32), nullable=True),
        sa.Column("ip_address", sa.String(64), nullable=True),
        sa.Column("note", sa.String(512), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
    )
    op.create_index("ix_ignored_hosts_mac_address", "ignored_hosts", ["mac_address"])
    op.create_index("ix_ignored_hosts_ip_address", "ignored_hosts", ["ip_address"])


def downgrade() -> None:
    op.drop_index("ix_ignored_hosts_ip_address", table_name="ignored_hosts")
    op.drop_index("ix_ignored_hosts_mac_address", table_name="ignored_hosts")
    op.drop_table("ignored_hosts")
