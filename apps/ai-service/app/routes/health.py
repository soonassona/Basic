"""Liveness and readiness probes."""

from __future__ import annotations

import time

from fastapi import APIRouter, Request

router = APIRouter(tags=["health"])


@router.get("/health")
def liveness(request: Request) -> dict[str, object]:
    started_at = getattr(request.app.state, "started_at", time.time())
    return {
        "status": "ok",
        "service": "visionloop-ai",
        "uptime_seconds": time.time() - started_at,
    }


@router.get("/ready")
def readiness() -> dict[str, str]:
    # Phase 2 will add Redis + RabbitMQ connectivity checks here.
    return {"status": "ready"}
