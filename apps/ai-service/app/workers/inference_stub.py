"""Phase 2 inference placeholder.

The real model loading + routing is Phase 3 (SAM 2.1 + YOLOv11). Until
then we return a deterministic mock result so the end-to-end flow
(submit → consume → callback → SSE) can be exercised without GPU.
"""

from __future__ import annotations

from typing import Any


def run_inference(envelope: dict[str, Any]) -> dict[str, Any]:
    """Return a mock result for *envelope*. Phase 3 replaces this with
    the SAM 2.1 / YOLOv11 router."""
    return {
        "model_used": "stub",
        "phase": 2,
        "type": envelope.get("type"),
        "image_id": envelope.get("image_id"),
        "ai_score": 0.0,
        "masks": [],
    }
