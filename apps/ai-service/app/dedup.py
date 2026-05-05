"""Cross-service dedup-key helper.

The Go API computes the dedup key for POST /v1/jobs as

    SHA-256( org_id || 0x1f || image_id || 0x1f || type || 0x1f || canonical(payload) )

with `canonical(payload)` being RFC 8785 JSON Canonicalization Scheme.
The AI worker (Phase 2 consumer task) re-computes this hash to verify
in-flight messages haven't been tampered with and to short-circuit
duplicate inference under retry. The two implementations MUST agree
byte-for-byte — see internal/domain/job.go (Go side).

We rely on Python's standard library only, since the consumer image
should not pull a dedicated JCS dep just for this. For our payload
shape (uuids, ints, strings, booleans, nested objects/arrays — never
floats) `json.dumps(..., sort_keys=True, separators=(',', ':'))` is
identical to RFC 8785 output. Floats would need ECMA-262 ToString
treatment; we assert against them.
"""

from __future__ import annotations

import hashlib
import json
from typing import Any
from uuid import UUID

_UNIT_SEPARATOR = b"\x1f"


def canonicalise_payload(payload: Any) -> bytes:
    """Return the RFC 8785 JCS encoding of *payload*.

    Accepts a dict / list / scalar already parsed from JSON. Raises
    ``TypeError`` if a float is encountered — callers must integerise
    or stringify floats so the hash stays portable across languages.
    """
    if payload is None:
        return b"{}"
    _reject_floats(payload)
    return json.dumps(
        payload,
        sort_keys=True,
        separators=(",", ":"),
        ensure_ascii=False,
        allow_nan=False,
    ).encode("utf-8")


def compute_dedup_key(
    org_id: str | UUID,
    image_id: str | UUID | None,
    job_type: str,
    canonical_payload: bytes,
) -> str:
    """Return the 64-char hex SHA-256 used by the jobs.dedup_key column."""
    h = hashlib.sha256()
    h.update(_uuid_str(org_id).encode("utf-8"))
    h.update(_UNIT_SEPARATOR)
    if image_id is not None:
        h.update(_uuid_str(image_id).encode("utf-8"))
    h.update(_UNIT_SEPARATOR)
    h.update(job_type.encode("utf-8"))
    h.update(_UNIT_SEPARATOR)
    h.update(canonical_payload)
    return h.hexdigest()


def _uuid_str(u: str | UUID) -> str:
    return str(u) if isinstance(u, UUID) else u


def _reject_floats(value: Any) -> None:
    if isinstance(value, float):
        raise TypeError(
            "dedup payloads must not contain floats — Go and Python "
            "serialise them differently. Use ints, strings, or fixed "
            "decimal representations."
        )
    if isinstance(value, dict):
        for v in value.values():
            _reject_floats(v)
    elif isinstance(value, list):
        for v in value:
            _reject_floats(v)
