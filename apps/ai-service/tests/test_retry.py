"""Retry-decision tests.

The retry topology lives on the API side (jobs.retry queue with DLX
back to jobs.pending) — these tests exercise the *consumer* side of
the contract: which attempts retry, which TTLs, when we DLQ.
"""

from __future__ import annotations

import json
from typing import Any

import httpx
import pytest

from app.workers.api_client import APIClient, CallbackAuth
from app.workers.jobs_consumer import handle_failure
from app.workers.retry_publisher import (
    BACKOFFS_MS,
    MAX_RETRIES,
    backoff_ms,
    schedule_retry,
    should_retry,
)


SECRET = b"dev-only-callback-hmac-32-bytes-padding-bb"


class FakeProducer:
    """Records every publish() call. Has the kombu Producer-shaped
    signature for `publish(body, ...)`."""

    def __init__(self) -> None:
        self.calls: list[dict[str, Any]] = []

    def publish(self, body: Any, **kwargs: Any) -> None:
        self.calls.append({"body": body, **kwargs})


class RecordingHTTP:
    def __init__(self) -> None:
        self.calls: list[dict] = []

    def __call__(self, req: httpx.Request) -> httpx.Response:
        self.calls.append(json.loads(req.content.decode()))
        return httpx.Response(200, json={})


def make_api() -> tuple[APIClient, RecordingHTTP]:
    rec = RecordingHTTP()
    api = APIClient(
        url="http://api/cb",
        auth=CallbackAuth(bearer="b", hmac_secret=SECRET),
        client=httpx.Client(transport=httpx.MockTransport(rec)),
    )
    return api, rec


# ---- pure-function policy tests --------------------------------------


def test_should_retry_within_window() -> None:
    assert should_retry(0)
    assert should_retry(1)
    assert should_retry(2)


def test_should_retry_rejects_final_attempt() -> None:
    assert not should_retry(MAX_RETRIES)
    assert not should_retry(MAX_RETRIES + 1)


def test_should_retry_rejects_negative() -> None:
    assert not should_retry(-1)


def test_backoff_schedule_matches_spec() -> None:
    assert BACKOFFS_MS == (1000, 4000, 16000)
    assert backoff_ms(0) == 1000
    assert backoff_ms(1) == 4000
    assert backoff_ms(2) == 16000


def test_backoff_raises_outside_window() -> None:
    with pytest.raises(ValueError):
        backoff_ms(MAX_RETRIES)


# ---- schedule_retry -----------------------------------------------------


def test_schedule_retry_publishes_to_retry_queue_with_ttl() -> None:
    p = FakeProducer()
    schedule_retry(p, {"job_id": "abc", "attempt": 0, "type": "auto"})

    assert len(p.calls) == 1
    call = p.calls[0]
    assert call["routing_key"] == "jobs.retry"
    assert call["expiration"] == "1000"
    assert call["delivery_mode"] == 2
    body = json.loads(call["body"])
    assert body["attempt"] == 1
    assert body["job_id"] == "abc"


def test_schedule_retry_increments_attempt_each_time() -> None:
    p = FakeProducer()
    schedule_retry(p, {"job_id": "abc", "attempt": 1})
    schedule_retry(p, {"job_id": "abc", "attempt": 2})

    ttls = [c["expiration"] for c in p.calls]
    assert ttls == ["4000", "16000"]
    bodies = [json.loads(c["body"])["attempt"] for c in p.calls]
    assert bodies == [2, 3]


def test_schedule_retry_refuses_when_exhausted() -> None:
    p = FakeProducer()
    with pytest.raises(ValueError):
        schedule_retry(p, {"job_id": "abc", "attempt": MAX_RETRIES})


# ---- handle_failure orchestration --------------------------------------


def test_handle_failure_attempt_zero_schedules_retry() -> None:
    api, rec = make_api()
    p = FakeProducer()
    disposition = handle_failure(
        {"job_id": "x", "attempt": 0}, api, p, RuntimeError("boom")
    )

    assert disposition == "retry"
    assert len(p.calls) == 1
    assert p.calls[0]["expiration"] == "1000"
    # No `failed` callback yet — row stays in `running` for SSE.
    assert all(c["state"] != "failed" for c in rec.calls)


def test_handle_failure_final_attempt_dlqs_with_failed_callback() -> None:
    api, rec = make_api()
    p = FakeProducer()
    disposition = handle_failure(
        {"job_id": "x", "attempt": MAX_RETRIES}, api, p, RuntimeError("boom")
    )

    assert disposition == "dlq"
    assert len(p.calls) == 0  # no retry publish past the window
    assert [c["state"] for c in rec.calls] == ["failed"]
    assert "RuntimeError" in rec.calls[0]["error"]
    assert "boom" in rec.calls[0]["error"]


def test_handle_failure_retry_publish_failure_falls_back_to_dlq() -> None:
    api, rec = make_api()

    class BrokenProducer(FakeProducer):
        def publish(self, *_a: Any, **_k: Any) -> None:
            raise OSError("broker offline")

    disposition = handle_failure(
        {"job_id": "x", "attempt": 0}, api, BrokenProducer(), RuntimeError("boom")
    )

    assert disposition == "dlq"
    assert [c["state"] for c in rec.calls] == ["failed"]
    assert "retry publish failed" in rec.calls[0]["error"]


def test_handle_failure_no_producer_dlqs_immediately() -> None:
    api, rec = make_api()
    disposition = handle_failure(
        {"job_id": "x", "attempt": 0}, api, None, RuntimeError("boom")
    )

    assert disposition == "dlq"
    assert [c["state"] for c in rec.calls] == ["failed"]
