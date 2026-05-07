from __future__ import annotations

import threading
import time
from concurrent.futures import ThreadPoolExecutor
from typing import Any

from app.config import Settings
from app.workers import yolo_stub
from app.workers.yolo_stub import YOLOBackend, YOLOPrediction


class CountingBackend:
    def __init__(self) -> None:
        self.calls = 0

    def predict(self, envelope: dict[str, Any]) -> YOLOPrediction:
        _ = envelope
        self.calls += 1
        return YOLOPrediction(boxes=[], ai_score=0.5)


class ConcurrencyBackend:
    def __init__(self) -> None:
        self._guard = threading.Lock()
        self.active = 0
        self.max_active = 0

    def predict(self, envelope: dict[str, Any]) -> YOLOPrediction:
        _ = envelope
        with self._guard:
            self.active += 1
            self.max_active = max(self.max_active, self.active)
        time.sleep(0.01)
        with self._guard:
            self.active -= 1
        return YOLOPrediction(boxes=[], ai_score=1.0)


def _settings() -> Settings:
    return Settings(
        YOLO_MODEL_ID="yolov11x",
        YOLO_MODEL_PATH="/tmp/yolov11x.onnx",
    )


def test_get_yolo_model_loads_once() -> None:
    yolo_stub._reset_yolo_singleton_for_tests()
    loads = 0

    def factory(_: Settings) -> YOLOBackend:
        nonlocal loads
        loads += 1
        return CountingBackend()

    first = yolo_stub.get_yolo_model(settings=_settings(), backend_factory=factory)
    second = yolo_stub.get_yolo_model(settings=_settings(), backend_factory=factory)

    assert first is second
    assert loads == 1


def test_yolo_inference_lock_serializes_parallel_calls() -> None:
    yolo_stub._reset_yolo_singleton_for_tests()
    backend = ConcurrencyBackend()

    def factory(_: Settings) -> YOLOBackend:
        return backend

    model = yolo_stub.get_yolo_model(settings=_settings(), backend_factory=factory)

    with ThreadPoolExecutor(max_workers=8) as pool:
        futures = [
            pool.submit(model.predict, {"type": "detect", "image_id": str(i)})
            for i in range(12)
        ]
        _ = [f.result() for f in futures]

    assert backend.max_active == 1


def test_run_yolo_inference_uses_singleton_instance() -> None:
    yolo_stub._reset_yolo_singleton_for_tests()
    loads = 0

    class FixedBackend:
        def predict(self, envelope: dict[str, Any]) -> YOLOPrediction:
            box = {"x1": 10.0, "y1": 20.0, "x2": 100.0, "y2": 200.0,
                   "confidence": 0.9, "class_id": 0}
            return YOLOPrediction(
                boxes=[box] if envelope.get("type") == "detect" else [],
                ai_score=0.9 if envelope.get("type") == "detect" else 0.0,
            )

    def factory(_: Settings) -> YOLOBackend:
        nonlocal loads
        loads += 1
        return FixedBackend()

    old_builder = yolo_stub._build_default_backend
    yolo_stub._build_default_backend = factory
    try:
        first = yolo_stub.run_yolo_inference({"type": "detect", "image_id": "img-1"})
        second = yolo_stub.run_yolo_inference({"type": "auto", "image_id": "img-2"})
    finally:
        yolo_stub._build_default_backend = old_builder

    assert loads == 1
    assert first["model_used"] == "yolov11x"
    assert second["model_used"] == "yolov11x"
    assert len(first["boxes"]) == 1
    assert first["boxes"][0]["confidence"] == 0.9
