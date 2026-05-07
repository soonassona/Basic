"""Integration tests: fixture ONNX models loaded by real ONNX Runtime (no mock).

Phase 3 exit criterion (spec §17): "all job types return masks for test images."
The fixture models are minimal constant-output ONNX graphs committed under
tests/fixtures/. They exercise the full OnnxSAMBackend / OnnxYOLOBackend
stack without requiring large model weights.

_fetch_image is mocked only to avoid network I/O; InferenceSession is real.
"""

from __future__ import annotations

from pathlib import Path
from unittest.mock import patch

import numpy as np
import pytest
from PIL import Image as PILImage

from app.workers import inference_stub, yolo_stub
from app.workers.inference_stub import SAMModel
from app.workers.model_router import route_inference
from app.workers.onnx_backends import OnnxSAMBackend, OnnxYOLOBackend
from app.workers.yolo_stub import YOLOModel


_FIXTURES = Path(__file__).parent / "fixtures"
_SAM_FIXTURE = _FIXTURES / "sam_fixture.onnx"
_YOLO_FIXTURE = _FIXTURES / "yolo_fixture.onnx"
_CPU = ["CPUExecutionProvider"]

_ENVELOPE = {"type": "box", "image_id": "img-fixture", "image_url": "http://x/img.png"}
_DUMMY_IMAGE = PILImage.new("RGB", (64, 64), color=(128, 64, 32))


# ── SAM fixture ───────────────────────────────────────────────────────────────

def test_sam_fixture_onnx_validates_on_cpu() -> None:
    """OnnxSAMBackend loads and validates the fixture without mocking InferenceSession."""
    backend = OnnxSAMBackend(str(_SAM_FIXTURE), _CPU)
    # _validate() ran inside __init__; reaching here means it passed
    assert backend is not None


def test_sam_fixture_onnx_predict_returns_masks() -> None:
    backend = OnnxSAMBackend(str(_SAM_FIXTURE), _CPU)
    with patch("app.workers.onnx_backends._fetch_image", return_value=_DUMMY_IMAGE):
        result = backend.predict(_ENVELOPE)
    assert isinstance(result.masks, list)
    assert len(result.masks) == 1          # fixture output: 1 mask
    assert result.masks[0]["mask_index"] == 0
    assert result.ai_score == pytest.approx(0.9)


# ── YOLO fixture ──────────────────────────────────────────────────────────────

def test_yolo_fixture_onnx_validates_on_cpu() -> None:
    """OnnxYOLOBackend loads and validates the fixture without mocking InferenceSession."""
    backend = OnnxYOLOBackend(str(_YOLO_FIXTURE), _CPU)
    assert backend is not None


def test_yolo_fixture_onnx_predict_returns_boxes() -> None:
    backend = OnnxYOLOBackend(str(_YOLO_FIXTURE), _CPU)
    env = {**_ENVELOPE, "type": "detect"}
    with patch("app.workers.onnx_backends._fetch_image", return_value=_DUMMY_IMAGE):
        result = backend.predict(env)
    assert len(result.boxes) == 2          # fixture has 2 detections above conf 0.25
    assert result.boxes[0]["confidence"] == pytest.approx(0.9)
    assert result.boxes[1]["confidence"] == pytest.approx(0.8)
    assert result.ai_score == pytest.approx(0.9)


# ── Full router with fixture backends ─────────────────────────────────────────

@pytest.fixture()
def sam_model():
    inference_stub._reset_sam_singleton_for_tests()
    backend = OnnxSAMBackend(str(_SAM_FIXTURE), _CPU)
    yield inference_stub.get_sam_model(backend_factory=lambda _: backend)
    inference_stub._reset_sam_singleton_for_tests()


@pytest.fixture()
def yolo_model():
    yolo_stub._reset_yolo_singleton_for_tests()
    backend = OnnxYOLOBackend(str(_YOLO_FIXTURE), _CPU)
    yield yolo_stub.get_yolo_model(backend_factory=lambda _: backend)
    yolo_stub._reset_yolo_singleton_for_tests()


@pytest.mark.parametrize("job_type,expected_model", [
    ("box",     "sam2.1_hiera_large"),
    ("points",  "sam2.1_hiera_large"),
    ("auto",    "sam2.1_hiera_large"),
    ("detect",  "yolov11x"),
    ("polygon", "manual"),
])
def test_all_job_types_return_results_via_fixture_models(
    sam_model: SAMModel,
    yolo_model: YOLOModel,
    job_type: str,
    expected_model: str,
) -> None:
    """Route all 5 job types through real fixture ONNX backends; each returns a result."""
    env = {"type": job_type, "image_id": "img-fixture", "image_url": "http://x/img.png"}
    with patch("app.workers.onnx_backends._fetch_image", return_value=_DUMMY_IMAGE):
        result = route_inference(env, sam=sam_model, yolo=yolo_model)
    assert result["model_used"] == expected_model
    assert result["image_id"] == "img-fixture"
    assert "ai_score" in result
