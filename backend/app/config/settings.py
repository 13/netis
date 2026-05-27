import os
from functools import lru_cache

from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict

# Persistent override file (lives on the data volume so changes survive image rebuilds).
# Override via NETIS_CONFIG_FILE env var for local dev.
_CONFIG_FILE = os.environ.get("NETIS_CONFIG_FILE", "/data/netis.env")


class Settings(BaseSettings):
    model_config = SettingsConfigDict(
        env_prefix="NETIS_",
        env_file=(".env", _CONFIG_FILE),  # later file wins
        env_file_encoding="utf-8",
        extra="ignore",
    )

    database_url: str = Field(default="sqlite:///./netis.db")
    secret_key: str = Field(default="dev-secret-key-change-me")
    algorithm: str = Field(default="HS256")
    access_token_expire_minutes: int = Field(default=60 * 24)

    cors_origins: str = Field(default="http://localhost:5173")

    app_name: str = Field(default="netis")
    debug: bool = Field(default=False)

    # Background scan scheduler
    scheduler_enabled: bool = Field(default=True)

    # Change notifications — POSTs JSON to this URL when new unknown hosts appear
    alert_webhook_url: str | None = Field(default=None)

    @property
    def cors_origin_list(self) -> list[str]:
        return [o.strip() for o in self.cors_origins.split(",") if o.strip()]


@lru_cache
def get_settings() -> Settings:
    return Settings()


def get_config_file_path() -> str:
    return _CONFIG_FILE
