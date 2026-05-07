"""Model router — dispatches inference envelopes to the right model(s).

Routing table (spec §5 Model Routing Logic):
  job_type = "auto"    → YOLOv11 → SAM 2.1 (YOLO boxes fed as SAM prompts)
  job_type = "box"     → SAM 2.1
  job_type = "points"  → SAM 2.1
  job_type = "polygon" → manual (no AI)
  job_type = "detect"  → YOLOv11 only (detection without segmentation)
  default              → SAM 2.1

route_inference() takes model instances as arguments so it remains a
pure, easily testable function. run_routed_inference() is the
process-level entry point that resolves the singletons.
"""

from __future__ import annotations

from typing import Any

from .inference_stub import SAMModel, get_sam_model
from .yolo_stub import YOLOModel, get_yolo_model


def route_inference(
    envelope: dict[str, Any],
    *,
    sam: SAMModel,
    yolo: YOLOModel,
) -> dict[str, Any]:
    job_type = envelope.get("type", "")

    if job_type == "auto":
        yolo_result = yolo.predict(envelope)
        sam_envelope = {**envelope, "boxes": yolo_result["boxes"]}
        sam_result = sam.predict(sam_envelope)
        return {**sam_result, "boxes": yolo_result["boxes"]}

    if job_type in ("box", "points"):
        return sam.predict(envelope)

    if job_type == "polygon":
        return {
            "model_used": "manual",
            "phase": 3,
            "type": "polygon",
            "image_id": envelope.get("image_id"),
            "ai_score": 0.0,
            "masks": [],
            "boxes": [],
        }

    if job_type == "detect":
        return yolo.predict(envelope)

    # default → SAM (covers unknown types)
    return sam.predict(envelope)


def run_routed_inference(envelope: dict[str, Any]) -> dict[str, Any]:
    """Process-level entry point — resolves singletons and routes."""
    return route_inference(envelope, sam=get_sam_model(), yolo=get_yolo_model())
