"""HTTP client for posting worker callbacks back to apps/api.

Wraps httpx + the HMAC signing required by apps/api's
/internal/jobs/callback endpoint (ADR-0004). Kept tiny and easy to
mock from tests.
"""

from __future__ import annotations

import hashlib
import hmac
import logging
from dataclasses import dataclass
from typing import Any

import httpx

log = logging.getLogger(__name__)


@dataclass(frozen=True)
class CallbackAuth:
    bearer: str
    hmac_secret: bytes


def sign_job_id(job_id: str, secret: bytes) -> str:
    """Hex HMAC-SHA256 of *job_id* under *secret*. Matches apps/api's
    middleware_callback.go — change one side, change the other."""
    return hmac.new(secret, job_id.encode("utf-8"), hashlib.sha256).hexdigest()


class APIClient:
    """Thin wrapper around the API gateway's /internal/jobs/callback.

    Constructor takes a configured httpx client so tests can inject a
    `httpx.MockTransport` without monkeypatching.
    """

    def __init__(self, url: str, auth: CallbackAuth, *, client: httpx.Client | None = None) -> None:
        self._url = url
        self._auth = auth
        self._client = client or httpx.Client(timeout=10.0)

    def post_callback(
        self,
        job_id: str,
        state: str,
        *,
        attempt: int = 0,
        error: str = "",
        result: dict[str, Any] | None = None,
    ) -> httpx.Response:
        payload: dict[str, Any] = {"state": state, "attempt": attempt}
        if error:
            payload["error"] = error
        if result is not None:
            payload["result"] = result

        headers = {
            "Authorization": f"Bearer {self._auth.bearer}",
            "X-Job-ID": job_id,
            "X-Job-Signature": sign_job_id(job_id, self._auth.hmac_secret),
            "Content-Type": "application/json",
        }
        resp = self._client.post(self._url, json=payload, headers=headers)
        if resp.status_code >= 400:
            log.warning("callback non-2xx", extra={"job_id": job_id, "status": resp.status_code, "body": resp.text})
        return resp

    def close(self) -> None:
        self._client.close()
