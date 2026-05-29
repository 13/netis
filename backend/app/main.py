import asyncio
import logging
from contextlib import asynccontextmanager
from pathlib import Path

from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import FileResponse
from fastapi.staticfiles import StaticFiles

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

    # Serve the compiled Vue SPA when running from the combined Docker image.
    # The static/ directory is only present inside the container (copied by the
    # root Dockerfile); dev mode (uvicorn --reload) skips this gracefully.
    static_dir = Path(__file__).parent.parent / "static"
    if static_dir.is_dir():
        # /assets, /favicon.ico, etc. — exact file matches
        app.mount("/assets", StaticFiles(directory=static_dir / "assets"), name="assets")

        # SPA fallback: every other path that doesn't match an API route returns
        # index.html so Vue Router can handle client-side navigation.
        @app.get("/{full_path:path}", include_in_schema=False)
        async def spa_fallback(full_path: str) -> FileResponse:
            return FileResponse(static_dir / "index.html")

    return app


app = create_app()
