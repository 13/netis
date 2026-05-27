import asyncio
import logging
from contextlib import asynccontextmanager

from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from app import __version__
from app.api import api_router
from app.config import get_settings
from app.database import Base, engine

logging.basicConfig(level=logging.INFO)
log = logging.getLogger(__name__)


def create_app() -> FastAPI:
    settings = get_settings()

    # When not using migrations (e.g. SQLite dev mode), ensure tables exist.
    # Alembic remains the source of truth for production schema changes.
    if settings.database_url.startswith("sqlite"):
        Base.metadata.create_all(bind=engine)

    @asynccontextmanager
    async def lifespan(_: FastAPI):
        task: asyncio.Task | None = None
        if settings.scheduler_enabled:
            from app.services.scheduler import scheduler_loop

            task = asyncio.create_task(scheduler_loop())
        try:
            yield
        finally:
            if task is not None:
                task.cancel()
                try:
                    await task
                except (asyncio.CancelledError, Exception):  # noqa: BLE001
                    pass

    app = FastAPI(
        title="netis",
        version=__version__,
        description="Lightweight self-hosted IPAM for homelabs",
        lifespan=lifespan,
    )

    app.add_middleware(
        CORSMiddleware,
        allow_origins=settings.cors_origin_list,
        allow_credentials=True,
        allow_methods=["*"],
        allow_headers=["*"],
    )

    app.include_router(api_router)

    @app.get("/api/health", tags=["meta"])
    def health() -> dict:
        return {"status": "ok", "version": __version__}

    return app


app = create_app()
