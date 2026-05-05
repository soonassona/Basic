"""FastAPI entrypoint for the VisionLoop ML service.

Phase 1 surface area:
  GET  /health        — liveness for orchestrators
  GET  /ready         — readiness; checks Redis + RabbitMQ in Phase 2
  GET  /v1/models     — registry stub (Phase 3 wires real model loading)
  POST /v1/infer      — returns 501 until Phase 3

The `infer_router` import path mirrors what Phase 3 wires up; today it
returns a typed "not implemented" envelope so the API gateway can shape
its job submission flow without waiting on the model implementation.
"""

from __future__ import annotations

import time
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager

import structlog
from fastapi import FastAPI

from .config import load_settings
from .routes import health, infer, models


@asynccontextmanager
async def lifespan(app: FastAPI) -> AsyncIterator[None]:
    settings = load_settings()
    app.state.settings = settings
    app.state.started_at = time.time()
    structlog.contextvars.bind_contextvars(service=settings.service_name)
    yield


def create_app() -> FastAPI:
    app = FastAPI(
        title="VisionLoop ML Service",
        version="0.1.0",
        docs_url="/docs",
        openapi_url="/openapi.json",
        lifespan=lifespan,
    )
    app.include_router(health.router)
    app.include_router(models.router, prefix="/v1")
    app.include_router(infer.router, prefix="/v1")
    return app


app = create_app()
