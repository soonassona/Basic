"""Model registry — Phase 3 will populate from R2/Postgres. Phase 1
returns a static manifest so the API gateway can wire its routing logic
against a stable shape."""

from __future__ import annotations

from fastapi import APIRouter

router = APIRouter(tags=["models"])


@router.get("/models")
def list_models() -> dict[str, object]:
    return {
        "models": [
            {
                "family": "sam",
                "id": "sam-2.1-hiera-large",
                "ready": False,
                "phase": 3,
                "role": "Primary segmentation engine.",
            },
            {
                "family": "yolo",
                "id": "yolov11x",
                "ready": False,
                "phase": 3,
                "role": "Object detection and pre-labeling.",
            },
        ],
    }
