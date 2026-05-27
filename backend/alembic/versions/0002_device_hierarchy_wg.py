"""Add parent_device_id and wg_pubkey to devices

Revision ID: 0002
Revises: 0001
Create Date: 2026-05-21

"""
from typing import Sequence, Union

import sqlalchemy as sa
from alembic import op

revision: str = "0002"
down_revision: Union[str, None] = "0001"
branch_labels: Union[str, Sequence[str], None] = None
depends_on: Union[str, Sequence[str], None] = None


def upgrade() -> None:
    with op.batch_alter_table("devices", schema=None) as batch_op:
        batch_op.add_column(sa.Column("wg_pubkey", sa.String(64), nullable=True))
        batch_op.add_column(sa.Column("parent_device_id", sa.Integer(), nullable=True))
        batch_op.create_index("ix_devices_wg_pubkey", ["wg_pubkey"], unique=True)
        batch_op.create_index("ix_devices_parent_device_id", ["parent_device_id"])
        batch_op.create_foreign_key(
            "fk_devices_parent_device_id",
            "devices",
            ["parent_device_id"],
            ["id"],
            ondelete="SET NULL",
        )


def downgrade() -> None:
    with op.batch_alter_table("devices", schema=None) as batch_op:
        batch_op.drop_constraint("fk_devices_parent_device_id", type_="foreignkey")
        batch_op.drop_index("ix_devices_parent_device_id")
        batch_op.drop_index("ix_devices_wg_pubkey")
        batch_op.drop_column("parent_device_id")
        batch_op.drop_column("wg_pubkey")
