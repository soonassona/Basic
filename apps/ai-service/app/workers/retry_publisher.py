"""Retry publisher for the AI worker.

Spec §11 / ADR-0004 (option A — single jobs.retry queue with per-
message TTL):

  attempt 0 fails → publish to jobs.retry with TTL=1000ms, attempt=1
  attempt 1 fails → publish to jobs.retry with TTL=4000ms, attempt=2
  attempt 2 fails → publish to jobs.retry with TTL=16000ms, attempt=3
  attempt 3 fails → reject without requeue → jobs.dead

The retry queue's x-dead-letter-exchange points back at the `jobs`
exchange with routing key `jobs.pending.retry`, which matches the
`jobs.pending.*` binding on `jobs.pending` — so an expired retry
re-arrives at the consumer with attempt incremented.
"""

from __future__ import annotations

import json
import logging
from typing import Any

from kombu import Connection, Exchange, Producer

# Wire constants — must match apps/api topology.go.
DEFAULT_EXCHANGE = ""  # nameless exchange routes by queue name
QUEUE_RETRY = "jobs.retry"

# Indexed by the attempt that JUST FAILED (0, 1, 2). attempt 3 fails =>
# DLQ, no entry. Kept tuple-typed so a stray future bug widening the
# list silently doesn't break the bounds check.
BACKOFFS_MS: tuple[int, ...] = (1000, 4000, 16000)
MAX_RETRIES = len(BACKOFFS_MS)

log = logging.getLogger(__name__)


def should_retry(attempt: int) -> bool:
    """True if a retry is allowed after attempt *attempt* failed."""
    return 0 <= attempt < MAX_RETRIES


def backoff_ms(attempt: int) -> int:
    """TTL to apply when scheduling the retry of *attempt*+1."""
    if not should_retry(attempt):
        raise ValueError(f"no backoff for attempt {attempt}; max is {MAX_RETRIES - 1}")
    return BACKOFFS_MS[attempt]


def schedule_retry(producer: Producer, envelope: dict[str, Any]) -> dict[str, Any]:
    """Publish a copy of *envelope* to jobs.retry with TTL set to the
    configured backoff for the attempt that just failed. Increments
    the envelope's `attempt` counter on the published copy and returns
    it for logging/auditing.
    """
    attempt = int(envelope.get("attempt", 0))
    if not should_retry(attempt):
        raise ValueError(
            f"refusing to publish retry for attempt {attempt} — DLQ path is the caller's job"
        )

    ttl = backoff_ms(attempt)
    next_envelope = dict(envelope)
    next_envelope["attempt"] = attempt + 1

    producer.publish(
        json.dumps(next_envelope),
        exchange=DEFAULT_EXCHANGE,
        routing_key=QUEUE_RETRY,
        content_type="application/json",
        delivery_mode=2,  # persistent
        expiration=str(ttl),  # ms; per-message TTL
        headers={
            "x-original-attempt": attempt,
            "x-job-id": envelope.get("job_id"),
            "x-backoff-ms": ttl,
        },
    )
    log.info(
        "scheduled retry",
        extra={
            "job_id": envelope.get("job_id"),
            "next_attempt": attempt + 1,
            "ttl_ms": ttl,
        },
    )
    return next_envelope


def open_producer(connection: Connection) -> Producer:
    """Build a Producer bound to *connection* with sane defaults for
    the retry path. Caller owns the lifecycle (Producer has no
    .close())."""
    return Producer(connection, exchange=Exchange(DEFAULT_EXCHANGE), serializer="json")
