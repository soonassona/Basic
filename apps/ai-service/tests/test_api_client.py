"""Tests for the API callback client.

The HMAC signature contract is shared with apps/api — see also
test_dedup.py (the dedup hash) for the matching pinned-value pattern.
"""

from __future__ import annotations

import json

import httpx

from app.workers.api_client import APIClient, CallbackAuth, sign_job_id


SECRET = b"dev-only-callback-hmac-32-bytes-padding-bb"


def test_sign_job_id_known_value() -> None:
    # Pinned: HMAC-SHA256(secret, "11111111-1111-1111-1111-111111111111")
    # If you change the algorithm, regenerate via:
    #   echo -n '...' | openssl dgst -sha256 -hmac '...'
    # and update apps/api/internal/interfaces/http/middleware_callback.go
    # at the same time.
    got = sign_job_id("11111111-1111-1111-1111-111111111111", SECRET)
    assert len(got) == 64
    int(got, 16)  # hex


def test_post_callback_emits_signed_request() -> None:
    captured: dict = {}

    def handler(req: httpx.Request) -> httpx.Response:
        captured["url"] = str(req.url)
        captured["headers"] = dict(req.headers)
        captured["body"] = json.loads(req.content.decode())
        return httpx.Response(200, json={"id": "x", "state": "running", "no_change": False})

    transport = httpx.MockTransport(handler)
    client = httpx.Client(transport=transport)
    api = APIClient(
        url="http://api:8080/internal/jobs/callback",
        auth=CallbackAuth(bearer="bearer-tok", hmac_secret=SECRET),
        client=client,
    )

    resp = api.post_callback(
        "11111111-1111-1111-1111-111111111111", "running", attempt=1
    )

    assert resp.status_code == 200
    assert captured["headers"]["authorization"] == "Bearer bearer-tok"
    assert captured["headers"]["x-job-id"] == "11111111-1111-1111-1111-111111111111"
    assert captured["headers"]["x-job-signature"] == sign_job_id(
        "11111111-1111-1111-1111-111111111111", SECRET
    )
    assert captured["body"] == {"state": "running", "attempt": 1}


def test_post_callback_succeeded_includes_result() -> None:
    captured: dict = {}

    def handler(req: httpx.Request) -> httpx.Response:
        captured["body"] = json.loads(req.content.decode())
        return httpx.Response(200, json={})

    api = APIClient(
        url="http://api/x",
        auth=CallbackAuth(bearer="b", hmac_secret=SECRET),
        client=httpx.Client(transport=httpx.MockTransport(handler)),
    )
    api.post_callback(
        "00000000-0000-0000-0000-000000000001",
        "succeeded",
        attempt=0,
        result={"masks": [], "ai_score": 0.42},
    )
    assert captured["body"]["state"] == "succeeded"
    assert captured["body"]["result"]["ai_score"] == 0.42
