"""Tests for OnnxSAMBackend and OnnxYOLOBackend.

onnxruntime.InferenceSession is mocked throughout — no model weights needed.
Tests verify: startup shape validation, correct inference output shapes,
and the _build_default_backend routing logic.
"""

from __future__ import annotations

import os
from typing import Any
from unittest.mock import MagicMock, patch

import numpy as np
import pytest

from app.config import Settings
from app.workers.onnx_backends import (
    OnnxSAMBackend,
    OnnxYOLOBackend,
    _parse_providers,
)
from app.workers import inference_stub, yolo_stub


# ── helpers ──────────────────────────────────────────────────────────────────

def _make_node(name: str, shape: list | None = None) -> MagicMock:
    node = MagicMock()
    node.name = name  # MagicMock(name=n) sets the mock's repr, not .name
    node.shape = shape or []
    return node


def _make_session(
    input_names: list[str],
    output_names: list[str],
    output_data: list[np.ndarray],
) -> MagicMock:
    session = MagicMock()
    session.get_inputs.return_value = [_make_node(n) for n in input_names]
    session.get_outputs.return_value = [
        _make_node(n, list(d.shape))
        for n, d in zip(output_names, output_data)
    ]
    session.run.return_value = output_data
    return session


def _sam_outputs() -> list[np.ndarray]:
    masks = np.zeros((1, 3, 1024, 1024), dtype=np.float32)
    iou = np.array([[0.9, 0.8, 0.7]], dtype=np.float32)
    return [masks, iou]


def _yolo_outputs(n_det: int = 2) -> list[np.ndarray]:
    dets = np.array(
        [[10.0, 20.0, 100.0, 200.0, 0.9, 0.0]] * n_det, dtype=np.float32
    )[np.newaxis]  # 1×n_det×6
    return [dets]


_SAM_ENVELOPE = {
    "type": "box",
    "image_id": "img-1",
    "image_url": "https://example.test/img.png",
}
_YOLO_ENVELOPE = {
    "type": "detect",
    "image_id": "img-1",
    "image_url": "https://example.test/img.png",
}

_DUMMY_IMAGE = b"\xff\xd8\xff\xe0" + b"\x00" * 100  # minimal JPEG-like bytes


# ── _parse_providers ──────────────────────────────────────────────────────────

def test_parse_providers_single() -> None:
    assert _parse_providers("CPUExecutionProvider") == ["CPUExecutionProvider"]


def test_parse_providers_multiple() -> None:
    result = _parse_providers("CUDAExecutionProvider,CPUExecutionProvider")
    assert result == ["CUDAExecutionProvider", "CPUExecutionProvider"]


# ── OnnxSAMBackend ────────────────────────────────────────────────────────────

def _make_sam_backend(session: MagicMock) -> OnnxSAMBackend:
    with patch("onnxruntime.InferenceSession", return_value=session):
        return OnnxSAMBackend(
            model_path="/tmp/sam.onnx",
            providers=["CPUExecutionProvider"],
        )


def test_sam_backend_raises_on_missing_input() -> None:
    session = _make_session(
        input_names=["wrong_input"],
        output_names=["masks", "iou_predictions"],
        output_data=_sam_outputs(),
    )
    with patch("onnxruntime.InferenceSession", return_value=session):
        with pytest.raises(ValueError, match="missing expected input 'image'"):
            OnnxSAMBackend("/tmp/sam.onnx", ["CPUExecutionProvider"])


def test_sam_backend_raises_on_missing_output() -> None:
    session = _make_session(
        input_names=["image"],
        output_names=["masks"],  # missing iou_predictions
        output_data=[np.zeros((1, 3, 1024, 1024), dtype=np.float32)],
    )
    with patch("onnxruntime.InferenceSession", return_value=session):
        with pytest.raises(ValueError, match="missing expected outputs"):
            OnnxSAMBackend("/tmp/sam.onnx", ["CPUExecutionProvider"])


def test_sam_backend_predict_returns_prediction() -> None:
    session = _make_session(
        input_names=["image"],
        output_names=["masks", "iou_predictions"],
        output_data=_sam_outputs(),
    )
    backend = _make_sam_backend(session)

    from PIL import Image as PILImage
    dummy_img = PILImage.new("RGB", (64, 64))
    with patch("app.workers.onnx_backends._fetch_image", return_value=dummy_img):
        result = backend.predict(_SAM_ENVELOPE)

    assert len(result.masks) == 3  # 3 masks from output shape (1,3,H,W)
    assert result.ai_score == pytest.approx(0.9)


# ── OnnxYOLOBackend ───────────────────────────────────────────────────────────

def _make_yolo_backend(session: MagicMock) -> OnnxYOLOBackend:
    with patch("onnxruntime.InferenceSession", return_value=session):
        return OnnxYOLOBackend(
            model_path="/tmp/yolo.onnx",
            providers=["CPUExecutionProvider"],
        )


def test_yolo_backend_raises_on_wrong_output_cols() -> None:
    bad_output = np.zeros((1, 10, 5), dtype=np.float32)  # 5 cols, not 6
    session = _make_session(
        input_names=["images"],
        output_names=["output0"],
        output_data=[bad_output],
    )
    with patch("onnxruntime.InferenceSession", return_value=session):
        with pytest.raises(ValueError, match="last dim expected 6"):
            OnnxYOLOBackend("/tmp/yolo.onnx", ["CPUExecutionProvider"])


def test_yolo_backend_predict_returns_boxes() -> None:
    session = _make_session(
        input_names=["images"],
        output_names=["output0"],
        output_data=_yolo_outputs(n_det=2),
    )
    backend = _make_yolo_backend(session)

    from PIL import Image as PILImage
    dummy_img = PILImage.new("RGB", (64, 64))
    with patch("app.workers.onnx_backends._fetch_image", return_value=dummy_img):
        result = backend.predict(_YOLO_ENVELOPE)

    assert len(result.boxes) == 2
    assert result.boxes[0]["confidence"] == pytest.approx(0.9)
    assert result.boxes[0]["x1"] == pytest.approx(10.0)
    assert result.ai_score == pytest.approx(0.9)


def test_yolo_backend_filters_low_confidence() -> None:
    low_conf = np.array([[[10.0, 20.0, 100.0, 200.0, 0.1, 0.0]]], dtype=np.float32)
    session = _make_session(
        input_names=["images"],
        output_names=["output0"],
        output_data=[low_conf],
    )
    backend = _make_yolo_backend(session)

    from PIL import Image as PILImage
    dummy_img = PILImage.new("RGB", (64, 64))
    with patch("app.workers.onnx_backends._fetch_image", return_value=dummy_img):
        result = backend.predict(_YOLO_ENVELOPE)

    assert result.boxes == []
    assert result.ai_score == 0.0


# ── _build_default_backend routing ───────────────────────────────────────────

def _settings(sam_path: str = "/tmp/no-such-sam.onnx",
              yolo_path: str = "/tmp/no-such-yolo.onnx") -> Settings:
    return Settings(SAM_MODEL_PATH=sam_path, YOLO_MODEL_PATH=yolo_path)


def test_sam_default_backend_is_stub_when_file_missing() -> None:
    inference_stub._reset_sam_singleton_for_tests()
    from app.workers.inference_stub import StubSAMBackend, _build_default_backend
    backend = _build_default_backend(_settings(sam_path="/tmp/no-such-file.onnx"))
    assert isinstance(backend, StubSAMBackend)


def test_sam_default_backend_is_onnx_when_file_exists(tmp_path: Any) -> None:
    fake = tmp_path / "sam.onnx"
    fake.write_bytes(b"fake")
    inference_stub._reset_sam_singleton_for_tests()

    mock_session = _make_session(
        input_names=["image"],
        output_names=["masks", "iou_predictions"],
        output_data=_sam_outputs(),
    )
    with patch("onnxruntime.InferenceSession", return_value=mock_session):
        from app.workers.inference_stub import _build_default_backend
        backend = _build_default_backend(_settings(sam_path=str(fake)))
    assert isinstance(backend, OnnxSAMBackend)


def test_yolo_default_backend_is_stub_when_file_missing() -> None:
    yolo_stub._reset_yolo_singleton_for_tests()
    from app.workers.yolo_stub import StubYOLOBackend, _build_default_backend
    backend = _build_default_backend(_settings(yolo_path="/tmp/no-such-file.onnx"))
    assert isinstance(backend, StubYOLOBackend)


def test_yolo_default_backend_is_onnx_when_file_exists(tmp_path: Any) -> None:
    fake = tmp_path / "yolo.onnx"
    fake.write_bytes(b"fake")
    yolo_stub._reset_yolo_singleton_for_tests()

    mock_session = _make_session(
        input_names=["images"],
        output_names=["output0"],
        output_data=_yolo_outputs(),
    )
    with patch("onnxruntime.InferenceSession", return_value=mock_session):
        from app.workers.yolo_stub import _build_default_backend
        backend = _build_default_backend(_settings(yolo_path=str(fake)))
    assert isinstance(backend, OnnxYOLOBackend)
