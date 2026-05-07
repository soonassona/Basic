from __future__ import annotations

import threading
import time
from concurrent.futures import ThreadPoolExecutor
from typing import Any

from app.config import Settings
from app.workers import inference_stub
from app.workers.inference_stub import SAMBackend, SAMPrediction


class CountingBackend(SAMBackend):
    def __init__(self) -> None:
        self.calls = 0

    def predict(self, envelope: dict[str, Any]) -> SAMPrediction:
        _ = envelope
        self.calls += 1
        return SAMPrediction(masks=[], ai_score=0.5)


class ConcurrencyBackend(SAMBackend):
    def __init__(self) -> None:
        self._guard = threading.Lock()
        self.active = 0
        self.max_active = 0

    def predict(self, envelope: dict[str, Any]) -> SAMPrediction:
        _ = envelope
        with self._guard:
            self.active += 1
            self.max_active = max(self.max_active, self.active)
        time.sleep(0.01)
        with self._guard:
            self.active -= 1
        return SAMPrediction(masks=[], ai_score=1.0)


def _settings() -> Settings:
    return Settings(
        SAM_MODEL_ID="sam2.1_hiera_large",
        SAM_MODEL_PATH="/tmp/sam2.1_hiera_large.onnx",
    )


def test_get_sam_model_loads_once() -> None:
    inference_stub._reset_sam_singleton_for_tests()
    loads = 0

    def factory(_: Settings) -> SAMBackend:
        nonlocal loads
        loads += 1
        return CountingBackend()

    first = inference_stub.get_sam_model(settings=_settings(), backend_factory=factory)
    second = inference_stub.get_sam_model(settings=_settings(), backend_factory=factory)

    assert first is second
    assert loads == 1


def test_sam_inference_lock_serializes_parallel_calls() -> None:
    inference_stub._reset_sam_singleton_for_tests()
    backend = ConcurrencyBackend()

    def factory(_: Settings) -> SAMBackend:
        return backend

    model = inference_stub.get_sam_model(settings=_settings(), backend_factory=factory)

    with ThreadPoolExecutor(max_workers=8) as pool:
        futures = [pool.submit(model.predict, {"type": "auto", "image_id": str(i)}) for i in range(12)]
        _ = [f.result() for f in futures]

    assert backend.max_active == 1


def test_run_inference_uses_singleton_instance() -> None:
    inference_stub._reset_sam_singleton_for_tests()
    loads = 0

    class FixedBackend(SAMBackend):
        def predict(self, envelope: dict[str, Any]) -> SAMPrediction:
            return SAMPrediction(
                masks=[],
                ai_score=0.0 if envelope.get("type") == "auto" else 1.0,
            )

    def factory(_: Settings) -> SAMBackend:
        nonlocal loads
        loads += 1
        return FixedBackend()

    old_builder = inference_stub._build_default_backend
    inference_stub._build_default_backend = factory
    try:
        first = inference_stub.run_inference({"type": "auto", "image_id": "img-1"})
        second = inference_stub.run_inference({"type": "box", "image_id": "img-2"})
    finally:
        inference_stub._build_default_backend = old_builder

    assert loads == 1
    assert first["model_used"] == "sam2.1_hiera_large"
    assert second["model_used"] == "sam2.1_hiera_large"
