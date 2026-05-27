"""add nmap observation source

Revision ID: 0003
Revises: 0002
Create Date: 2026-05-22
"""
from typing import Sequence, Union

import sqlalchemy as sa
from alembic import op

revision: str = "0003"
down_revision: Union[str, None] = "0002"
branch_labels: Union[str, Sequence[str], None] = None
depends_on: Union[str, Sequence[str], None] = None


def upgrade() -> None:
    conn = op.get_bind()
    if conn.dialect.name == "postgresql":
        conn.execute(sa.text("ALTER TYPE observation_source ADD VALUE IF NOT EXISTS 'nmap'"))


def downgrade() -> None:
    # PostgreSQL does not support removing enum values; no-op
    pass
