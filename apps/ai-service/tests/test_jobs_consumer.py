"""Pure-function tests for the consumer's per-message logic.

We don't spin up a real broker here — kombu wiring is exercised in the
end-to-end integration test (separate row). What we DO verify: given an
envelope and an api_client double, process_envelope posts running →
inference → succeeded in order, and a failure path posts failed.
"""

from __future__ import annotations

import json

import httpx
import pytest

from app.workers.api_client import APIClient, CallbackAuth
from app.workers.jobs_consumer import process_envelope


SECRET = b"dev-only-callback-hmac-32-bytes-padding-bb"


class RecordingClient:
    """Capture every callback POST in order."""

    def __init__(self, status: int = 200) -> None:
        self.calls: list[dict] = []
        self._status = status

    def __call__(self, req: httpx.Request) -> httpx.Response:
        self.calls.append(json.loads(req.content.decode()))
        return httpx.Response(self._status, json={"id": "x", "state": "ok"})


def make_api(handler: RecordingClient) -> APIClient:
    return APIClient(
        url="http://api/cb",
        auth=CallbackAuth(bearer="b", hmac_secret=SECRET),
        client=httpx.Client(transport=httpx.MockTransport(handler)),
    )


def envelope(job_id: str = "11111111-1111-1111-1111-111111111111") -> dict:
    return {
        "job_id": job_id,
        "org_id": "22222222-2222-2222-2222-222222222222",
        "image_id": "33333333-3333-3333-3333-333333333333",
        "type": "auto",
        "attempt": 0,
        "dedup_key": "x",
        "payload": {},
    }


def test_process_envelope_running_then_succeeded() -> None:
    rec = RecordingClient()
    api = make_api(rec)

    result = process_envelope(envelope(), api)

    assert [c["state"] for c in rec.calls] == ["running", "succeeded"]
    assert rec.calls[1]["result"] == result
    assert result["model_used"] == "stub"
    assert result["type"] == "auto"


def test_process_envelope_inference_failure_raises(monkeypatch: pytest.MonkeyPatch) -> None:
    """process_envelope no longer posts `failed` itself — handle_failure
    decides retry vs DLQ. Here we just verify the running callback fired
    and the inference exception propagates."""
    from app.workers import jobs_consumer

    def boom(_envelope: dict) -> dict:
        raise RuntimeError("model exploded")

    monkeypatch.setattr(jobs_consumer, "run_inference", boom)

    rec = RecordingClient()
    api = make_api(rec)

    with pytest.raises(RuntimeError):
        process_envelope(envelope(), api)

    assert [c["state"] for c in rec.calls] == ["running"]


def test_process_envelope_attempt_propagated() -> None:
    rec = RecordingClient()
    api = make_api(rec)
    env = envelope()
    env["attempt"] = 3

    process_envelope(env, api)

    assert all(c["attempt"] == 3 for c in rec.calls)


def test_process_envelope_callback_5xx_raises() -> None:
    rec = RecordingClient(status=503)
    api = make_api(rec)
    with pytest.raises(httpx.HTTPStatusError):
        process_envelope(envelope(), api)
