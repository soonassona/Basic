"""RabbitMQ consumer for the jobs.pending queue.

The API gateway publishes to `jobs` exchange (topic) with routing key
`jobs.pending.{type}`; both queue and binding are declared on the API
side (see apps/api/internal/infrastructure/queue/topology.go). This
consumer reads the queue, runs inference (Phase 2: stub), and posts
the result back to /internal/jobs/callback.

Acknowledgement contract:
  • running callback        → before inference (lets SSE clients see it)
  • succeeded/failed cb     → after inference
  • broker ack              → only after the *terminal* callback returns
                              2xx, so a worker crash mid-flight leaves
                              the message un-acked → it gets redelivered
                              after the channel closes.

Design choice: callbacks are POSTed synchronously from the consumer
thread. The retry-policy task (separate TODO row) will introduce the
nack-with-requeue path that lifts a failed message into jobs.retry.
"""

from __future__ import annotations

import json
import logging
import signal
import threading
from collections.abc import Callable
from typing import Any

from kombu import Connection, Consumer, Producer, Queue
from kombu.message import Message

from ..config import load_settings
from .api_client import APIClient, CallbackAuth
from .model_router import run_routed_inference
from .inference_stub import get_sam_model
from .yolo_stub import get_yolo_model
from .retry_publisher import open_producer, schedule_retry, should_retry

log = logging.getLogger(__name__)

# These names are wire constants — they MUST agree with
# apps/api/internal/infrastructure/queue/topology.go.
QUEUE_PENDING = "jobs.pending"


def process_envelope(envelope: dict[str, Any], api: APIClient) -> dict[str, Any]:
    """Pure side-effecting unit: posts running, runs inference, posts
    terminal. Returns the dict that was sent in the terminal callback,
    so tests can assert on the wire shape without touching kombu.
    Raises if inference or any callback fails (caller decides whether
    to schedule a retry or DLQ).
    """
    job_id = envelope["job_id"]
    attempt = int(envelope.get("attempt", 0))

    api.post_callback(job_id, "running", attempt=attempt).raise_for_status()
    result = run_routed_inference(envelope)
    api.post_callback(job_id, "succeeded", attempt=attempt, result=result).raise_for_status()
    return result


def handle_failure(
    envelope: dict[str, Any],
    api: APIClient,
    producer: Producer | None,
    exc: BaseException,
) -> str:
    """Decide what to do with a message whose processing raised.

    Returns one of "retry" or "dlq" describing the disposition.

    Retry path: republish to jobs.retry with TTL backoff and increment
    the attempt counter. The caller acks the *original* delivery — the
    new message is a fresh delivery once the TTL expires.

    DLQ path: caller rejects without requeue; the broker dead-letters
    via the topology DLX into jobs.dead. We also post a terminal
    `failed` callback so the SSE client + audit log capture it.
    """
    attempt = int(envelope.get("attempt", 0))
    job_id = envelope.get("job_id", "")
    err = f"{type(exc).__name__}: {exc!s}"

    if should_retry(attempt) and producer is not None:
        try:
            schedule_retry(producer, envelope)
        except Exception:
            # If retry publish itself fails the only honest move is DLQ
            # the original — retrying-the-retry would just hide bugs.
            log.exception("retry publish failed; falling through to DLQ", extra={"job_id": job_id})
            api.post_callback(job_id, "failed", attempt=attempt, error=f"{err} (retry publish failed)")
            return "dlq"
        # Surface the in-flight failure so SSE clients see we're retrying.
        # The DB row stays in `running` until the next attempt — we
        # don't transition to `failed` here because that would be
        # terminal in the API's state machine.
        return "retry"

    # Final attempt or producer missing: terminal failure → DLQ.
    api.post_callback(job_id, "failed", attempt=attempt, error=err)
    return "dlq"


def _make_handler(api: APIClient, producer: Producer | None) -> Callable[[Any, Message], None]:
    """Build the kombu callback. On success, ack. On failure, dispatch
    to handle_failure → either republish to jobs.retry (then ack the
    original) or reject without requeue (DLQ via topology DLX)."""

    def _on_message(body: Any, message: Message) -> None:
        try:
            envelope = body if isinstance(body, dict) else json.loads(body)
        except Exception as exc:  # noqa: BLE001
            log.exception("malformed envelope; rejecting", extra={"reason": repr(exc)})
            message.reject(requeue=False)
            return

        try:
            process_envelope(envelope, api)
        except Exception as exc:  # noqa: BLE001
            disposition = handle_failure(envelope, api, producer, exc)
            if disposition == "retry":
                message.ack()
            else:
                message.reject(requeue=False)
            return
        message.ack()

    return _on_message


def run() -> None:
    """Entrypoint. Blocks until SIGTERM/SIGINT."""
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s")
    settings = load_settings()
    # Fail fast if either model cannot be initialized; inference reuses
    # the process singletons created here.
    get_sam_model(settings=settings)
    get_yolo_model(settings=settings)
    api = APIClient(
        url=settings.api_callback_url,
        auth=CallbackAuth(
            bearer=settings.job_callback_token,
            hmac_secret=settings.job_callback_hmac_secret.encode("utf-8"),
        ),
    )

    # passive=True: the queue is declared by the API on boot. We refuse
    # to declare here so a topology mismatch fails loudly rather than
    # silently creating a divergent queue.
    queue = Queue(QUEUE_PENDING, no_declare=True)

    stop = threading.Event()

    def _handle_signal(*_: Any) -> None:
        log.info("shutdown signal received")
        stop.set()

    signal.signal(signal.SIGTERM, _handle_signal)
    signal.signal(signal.SIGINT, _handle_signal)

    with Connection(settings.rabbitmq_url, heartbeat=30) as conn:
        log.info("consumer connected", extra={"url": settings.rabbitmq_url})
        producer = open_producer(conn)
        with Consumer(conn, queues=[queue], callbacks=[_make_handler(api, producer)], prefetch_count=1):
            while not stop.is_set():
                try:
                    conn.drain_events(timeout=1.0)
                except OSError:
                    # drain_events raises socket.timeout (an OSError) when
                    # idle — fall through to re-check stop and continue.
                    continue
    api.close()


if __name__ == "__main__":
    run()
