from __future__ import annotations

from typing import Any

from app.workers import inference_stub, yolo_stub
from app.workers.inference_stub import SAMBackend, SAMPrediction
from app.workers.model_router import route_inference
from app.workers.yolo_stub import YOLOBackend, YOLOPrediction
from app.config import Settings


_BOX = {"x1": 10.0, "y1": 20.0, "x2": 100.0, "y2": 200.0, "confidence": 0.9, "class_id": 0}
_MASK = {"polygon": [[0, 0, 1, 0, 1, 1]], "label": "object"}


class TrackingYOLO:
    def __init__(self, boxes: list[dict[str, Any]] | None = None) -> None:
        self.calls = 0
        self._boxes = boxes or [_BOX]

    def predict(self, envelope: dict[str, Any]) -> dict[str, Any]:
        self.calls += 1
        return {
            "model_used": "yolov11x",
            "phase": 3,
            "type": envelope.get("type"),
            "image_id": envelope.get("image_id"),
            "ai_score": 0.9,
            "boxes": self._boxes,
        }


class TrackingSAM:
    def __init__(self) -> None:
        self.calls = 0
        self.last_envelope: dict[str, Any] | None = None

    def predict(self, envelope: dict[str, Any]) -> dict[str, Any]:
        self.calls += 1
        self.last_envelope = envelope
        return {
            "model_used": "sam2.1_hiera_large",
            "phase": 3,
            "type": envelope.get("type"),
            "image_id": envelope.get("image_id"),
            "ai_score": 0.85,
            "masks": [_MASK],
        }


def _envelope(job_type: str) -> dict[str, Any]:
    return {"type": job_type, "image_id": "img-1", "job_id": "job-1"}


def test_auto_calls_yolo_then_sam_and_merges() -> None:
    yolo, sam = TrackingYOLO(), TrackingSAM()
    result = route_inference(_envelope("auto"), sam=sam, yolo=yolo)

    assert yolo.calls == 1
    assert sam.calls == 1
    # SAM receives the YOLO boxes in its envelope
    assert sam.last_envelope is not None
    assert sam.last_envelope["boxes"] == [_BOX]
    # Result merges SAM masks + YOLO boxes
    assert result["masks"] == [_MASK]
    assert result["boxes"] == [_BOX]
    assert result["model_used"] == "sam2.1_hiera_large"


def test_box_calls_sam_only() -> None:
    yolo, sam = TrackingYOLO(), TrackingSAM()
    result = route_inference(_envelope("box"), sam=sam, yolo=yolo)

    assert sam.calls == 1
    assert yolo.calls == 0
    assert result["model_used"] == "sam2.1_hiera_large"


def test_points_calls_sam_only() -> None:
    yolo, sam = TrackingYOLO(), TrackingSAM()
    result = route_inference(_envelope("points"), sam=sam, yolo=yolo)

    assert sam.calls == 1
    assert yolo.calls == 0
    assert result["model_used"] == "sam2.1_hiera_large"


def test_polygon_calls_no_model() -> None:
    yolo, sam = TrackingYOLO(), TrackingSAM()
    result = route_inference(_envelope("polygon"), sam=sam, yolo=yolo)

    assert sam.calls == 0
    assert yolo.calls == 0
    assert result["model_used"] == "manual"
    assert result["masks"] == []
    assert result["boxes"] == []


def test_detect_calls_yolo_only() -> None:
    yolo, sam = TrackingYOLO(), TrackingSAM()
    result = route_inference(_envelope("detect"), sam=sam, yolo=yolo)

    assert yolo.calls == 1
    assert sam.calls == 0
    assert result["model_used"] == "yolov11x"
    assert result["boxes"] == [_BOX]


def test_unknown_type_falls_through_to_sam() -> None:
    yolo, sam = TrackingYOLO(), TrackingSAM()
    result = route_inference(_envelope("unknown_future_type"), sam=sam, yolo=yolo)

    assert sam.calls == 1
    assert yolo.calls == 0
    assert result["model_used"] == "sam2.1_hiera_large"
