"""add subnet scan scheduling settings

Revision ID: 0005
Revises: 0004
Create Date: 2026-05-22
"""
from typing import Sequence, Union

import sqlalchemy as sa
from alembic import op

revision: str = "0005"
down_revision: Union[str, None] = "0004"
branch_labels: Union[str, Sequence[str], None] = None
depends_on: Union[str, Sequence[str], None] = None


def upgrade() -> None:
    with op.batch_alter_table("subnets") as batch:
        batch.add_column(
            sa.Column("scan_enabled", sa.Boolean(), nullable=False, server_default=sa.false())
        )
        batch.add_column(sa.Column("scan_interval_minutes", sa.Integer(), nullable=True))
        batch.add_column(
            sa.Column("scan_method", sa.String(8), nullable=False, server_default="arp")
        )
        batch.add_column(sa.Column("last_scanned_at", sa.DateTime(timezone=True), nullable=True))


def downgrade() -> None:
    with op.batch_alter_table("subnets") as batch:
        batch.drop_column("last_scanned_at")
        batch.drop_column("scan_method")
        batch.drop_column("scan_interval_minutes")
        batch.drop_column("scan_enabled")
