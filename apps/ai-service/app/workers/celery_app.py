"""Celery skeleton wired to Redis. Tasks land in Phase 2."""

from __future__ import annotations

from celery import Celery

from ..config import load_settings

_settings = load_settings()

celery_app = Celery(
    "visionloop_ai",
    broker=_settings.redis_url,
    backend=_settings.redis_url,
    include=[],
)

celery_app.conf.update(
    task_acks_late=True,
    task_reject_on_worker_lost=True,
    worker_prefetch_multiplier=1,
    task_default_queue="ai.default",
)
