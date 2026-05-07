"""SAM 2.1 singleton runtime.

Phase 3 requirement: load SAM 2.1 once per worker process and guard
inference with a thread-safe lock.

The default backend remains lightweight (deterministic placeholder) so
CI can validate lifecycle/concurrency behavior without large model files.
"""

from __future__ import annotations

from dataclasses import dataclass
import os
import threading
from typing import Any, Callable, Protocol

from ..config import Settings, load_settings


@dataclass(frozen=True)
class SAMPrediction:
    masks: list[dict[str, Any]]
    ai_score: float


class SAMBackend(Protocol):
    def predict(self, envelope: dict[str, Any]) -> SAMPrediction:
        """Run SAM inference for one envelope."""


class StubSAMBackend:
    """Deterministic backend used until the real SAM runtime lands."""

    def __init__(self, model_path: str) -> None:
        self.model_path = model_path

    def predict(self, envelope: dict[str, Any]) -> SAMPrediction:
        _ = envelope
        return SAMPrediction(masks=[], ai_score=0.0)


class SAMModel:
    """Process-wide SAM model wrapper with a per-inference lock."""

    def __init__(self, model_id: str, model_path: str, backend: SAMBackend) -> None:
        self.model_id = model_id
        self.model_path = model_path
        self._backend = backend
        self._inference_lock = threading.Lock()

    def predict(self, envelope: dict[str, Any]) -> dict[str, Any]:
        with self._inference_lock:
            out = self._backend.predict(envelope)

        return {
            "model_used": self.model_id,
            "phase": 3,
            "type": envelope.get("type"),
            "image_id": envelope.get("image_id"),
            "ai_score": out.ai_score,
            "masks": out.masks,
        }


_singleton_lock = threading.Lock()
_sam_singleton: SAMModel | None = None
_BackendFactory = Callable[[Settings], SAMBackend]


def _build_default_backend(settings: Settings) -> SAMBackend:
    if os.path.exists(settings.sam_model_path):
        from .onnx_backends import OnnxSAMBackend, _parse_providers
        return OnnxSAMBackend(
            model_path=settings.sam_model_path,
            providers=_parse_providers(settings.onnx_providers),
        )
    return StubSAMBackend(model_path=settings.sam_model_path)


def get_sam_model(
    *,
    settings: Settings | None = None,
    backend_factory: _BackendFactory | None = None,
) -> SAMModel:
    """Return a process singleton SAM runtime, creating it once."""
    global _sam_singleton

    resolved_settings = settings or load_settings()
    resolved_factory = backend_factory or _build_default_backend

    if _sam_singleton is not None:
        return _sam_singleton

    with _singleton_lock:
        if _sam_singleton is None:
            backend = resolved_factory(resolved_settings)
            _sam_singleton = SAMModel(
                model_id=resolved_settings.sam_model_id,
                model_path=resolved_settings.sam_model_path,
                backend=backend,
            )
    return _sam_singleton


def run_inference(envelope: dict[str, Any]) -> dict[str, Any]:
    """Route one envelope through the SAM singleton runtime."""
    return get_sam_model().predict(envelope)


def _reset_sam_singleton_for_tests() -> None:
    """Testing hook to isolate singleton state between tests."""
    global _sam_singleton
    with _singleton_lock:
        _sam_singleton = None
