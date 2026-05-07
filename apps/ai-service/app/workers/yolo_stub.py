"""YOLOv11x singleton runtime.

Phase 3 requirement: load YOLOv11x once per worker process and guard
inference with a thread-safe lock.

The default backend remains lightweight (deterministic placeholder) so
CI can validate lifecycle/concurrency behavior without large model files.

Box schema matches the SAM bbox prompt contract so the model router
(next TODO item) can pass YOLO output directly to SAM without
transformation:
    {"x1": float, "y1": float, "x2": float, "y2": float,
     "confidence": float, "class_id": int}
"""

from __future__ import annotations

from dataclasses import dataclass, field
import os
import threading
from typing import Any, Callable, Protocol

from ..config import Settings, load_settings


@dataclass(frozen=True)
class YOLOPrediction:
    boxes: list[dict[str, Any]]
    ai_score: float


class YOLOBackend(Protocol):
    def predict(self, envelope: dict[str, Any]) -> YOLOPrediction:
        """Run YOLO inference for one envelope."""


class StubYOLOBackend:
    """Deterministic backend used until the real YOLO runtime lands."""

    def __init__(self, model_path: str) -> None:
        self.model_path = model_path

    def predict(self, envelope: dict[str, Any]) -> YOLOPrediction:
        _ = envelope
        return YOLOPrediction(boxes=[], ai_score=0.0)


@dataclass
class YOLOModel:
    """Process-wide YOLO model wrapper with a per-inference lock."""

    model_id: str
    model_path: str
    _backend: YOLOBackend = field(repr=False)
    _inference_lock: threading.Lock = field(default_factory=threading.Lock, repr=False)

    def predict(self, envelope: dict[str, Any]) -> dict[str, Any]:
        with self._inference_lock:
            out = self._backend.predict(envelope)

        return {
            "model_used": self.model_id,
            "phase": 3,
            "type": envelope.get("type"),
            "image_id": envelope.get("image_id"),
            "ai_score": out.ai_score,
            "boxes": out.boxes,
        }


_singleton_lock = threading.Lock()
_yolo_singleton: YOLOModel | None = None
_BackendFactory = Callable[[Settings], YOLOBackend]


def _build_default_backend(settings: Settings) -> YOLOBackend:
    if os.path.exists(settings.yolo_model_path):
        from .onnx_backends import OnnxYOLOBackend, _parse_providers
        return OnnxYOLOBackend(
            model_path=settings.yolo_model_path,
            providers=_parse_providers(settings.onnx_providers),
        )
    return StubYOLOBackend(model_path=settings.yolo_model_path)


def get_yolo_model(
    *,
    settings: Settings | None = None,
    backend_factory: _BackendFactory | None = None,
) -> YOLOModel:
    """Return a process singleton YOLO runtime, creating it once."""
    global _yolo_singleton

    resolved_settings = settings or load_settings()
    resolved_factory = backend_factory or _build_default_backend

    if _yolo_singleton is not None:
        return _yolo_singleton

    with _singleton_lock:
        if _yolo_singleton is None:
            backend = resolved_factory(resolved_settings)
            _yolo_singleton = YOLOModel(
                model_id=resolved_settings.yolo_model_id,
                model_path=resolved_settings.yolo_model_path,
                _backend=backend,
            )
    return _yolo_singleton


def run_yolo_inference(envelope: dict[str, Any]) -> dict[str, Any]:
    """Route one envelope through the YOLO singleton runtime."""
    return get_yolo_model().predict(envelope)


def _reset_yolo_singleton_for_tests() -> None:
    """Testing hook to isolate singleton state between tests."""
    global _yolo_singleton
    with _singleton_lock:
        _yolo_singleton = None
