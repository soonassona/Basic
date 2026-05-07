"""Real ONNX Runtime backends for SAM 2.1 and YOLOv11x (spec §5).

These replace StubSAMBackend / StubYOLOBackend when model files are
present at startup. CI and environments without model weights continue
using the stub backends transparently.

Startup contract (CLAUDE.md — dimension contracts):
  Each backend asserts expected input/output names on session creation
  and raises ValueError immediately rather than failing silently at
  inference time.

Input pipeline:
  Both backends accept an envelope dict with an "image_url" key.
  The image is fetched via HTTP, decoded with PIL, and resized to the
  model's expected spatial dimensions before inference.
"""

from __future__ import annotations

import io
import urllib.request
from typing import Any

import numpy as np
from PIL import Image

from .inference_stub import SAMBackend, SAMPrediction
from .yolo_stub import YOLOBackend, YOLOPrediction

# SAM expects 1024×1024 RGB input; output names are fixed at export time.
_SAM_INPUT_SIZE = (1024, 1024)
_SAM_INPUT_NAME = "image"
_SAM_REQUIRED_OUTPUTS = {"masks", "iou_predictions"}

# YOLO expects 640×640 RGB input; each detection row is [x1,y1,x2,y2,conf,cls].
_YOLO_INPUT_SIZE = (640, 640)
_YOLO_DETECTION_COLS = 6
_YOLO_CONF_THRESHOLD = 0.25


def _parse_providers(providers_str: str) -> list[str]:
    """Split 'CPUExecutionProvider,CUDAExecutionProvider' → list."""
    return [p.strip() for p in providers_str.split(",") if p.strip()]


def _fetch_image(url: str) -> Image.Image:
    with urllib.request.urlopen(url, timeout=30) as resp:  # noqa: S310
        return Image.open(io.BytesIO(resp.read())).convert("RGB")


def _image_to_nchw(img: Image.Image, size: tuple[int, int]) -> np.ndarray:
    """Resize, normalize to [0,1], and return float32 NCHW array."""
    arr = np.array(img.resize(size), dtype=np.float32) / 255.0
    return arr.transpose(2, 0, 1)[np.newaxis]  # 1×3×H×W


class OnnxSAMBackend:
    """SAM 2.1 inference via a single end-to-end ONNX session."""

    def __init__(self, model_path: str, providers: list[str]) -> None:
        try:
            import onnxruntime as ort
        except ImportError as exc:
            raise ImportError(
                "onnxruntime is required for ONNX inference. "
                "Install with: pip install -e '.[ml]'"
            ) from exc

        self._session = ort.InferenceSession(model_path, providers=providers)
        self._validate()

    def _validate(self) -> None:
        input_names = {i.name for i in self._session.get_inputs()}
        output_names = {o.name for o in self._session.get_outputs()}

        if _SAM_INPUT_NAME not in input_names:
            raise ValueError(
                f"SAM ONNX model missing expected input '{_SAM_INPUT_NAME}'. "
                f"Found: {input_names}"
            )
        missing = _SAM_REQUIRED_OUTPUTS - output_names
        if missing:
            raise ValueError(
                f"SAM ONNX model missing expected outputs: {missing}. "
                f"Found: {output_names}"
            )

    def predict(self, envelope: dict[str, Any]) -> SAMPrediction:
        image_url = envelope.get("image_url", "")
        img = _fetch_image(image_url)
        image_tensor = _image_to_nchw(img, _SAM_INPUT_SIZE)

        outputs = self._session.run(
            ["masks", "iou_predictions"],
            {_SAM_INPUT_NAME: image_tensor},
        )
        raw_masks: np.ndarray = outputs[0]   # 1×N×H×W
        iou_preds: np.ndarray = outputs[1]   # 1×N

        masks = [
            {"mask_index": i, "iou": float(iou_preds[0, i])}
            for i in range(raw_masks.shape[1])
        ]
        ai_score = float(iou_preds[0].max()) if iou_preds.size > 0 else 0.0
        return SAMPrediction(masks=masks, ai_score=ai_score)


class OnnxYOLOBackend:
    """YOLOv11x detection inference via ONNX Runtime."""

    def __init__(self, model_path: str, providers: list[str]) -> None:
        try:
            import onnxruntime as ort
        except ImportError as exc:
            raise ImportError(
                "onnxruntime is required for ONNX inference. "
                "Install with: pip install -e '.[ml]'"
            ) from exc

        self._session = ort.InferenceSession(model_path, providers=providers)
        self._input_name = self._session.get_inputs()[0].name
        self._validate()

    def _validate(self) -> None:
        outputs = self._session.get_outputs()
        if not outputs:
            raise ValueError("YOLO ONNX model has no outputs.")

        shape = outputs[0].shape
        # shape is typically [batch, num_detections, 6] or [batch, 6, num_det]
        if shape and len(shape) == 3 and shape[-1] != _YOLO_DETECTION_COLS:
            raise ValueError(
                f"YOLO ONNX output last dim expected {_YOLO_DETECTION_COLS}, "
                f"got {shape[-1]}. Shape: {shape}"
            )

    def predict(self, envelope: dict[str, Any]) -> YOLOPrediction:
        image_url = envelope.get("image_url", "")
        img = _fetch_image(image_url)
        image_tensor = _image_to_nchw(img, _YOLO_INPUT_SIZE)

        raw: np.ndarray = self._session.run(None, {self._input_name: image_tensor})[0]
        # raw shape: [1, num_detections, 6] — x1 y1 x2 y2 conf cls
        detections = raw[0] if raw.ndim == 3 else raw

        boxes = [
            {
                "x1": float(det[0]),
                "y1": float(det[1]),
                "x2": float(det[2]),
                "y2": float(det[3]),
                "confidence": float(det[4]),
                "class_id": int(det[5]),
            }
            for det in detections
            if float(det[4]) >= _YOLO_CONF_THRESHOLD
        ]
        ai_score = max((b["confidence"] for b in boxes), default=0.0)
        return YOLOPrediction(boxes=boxes, ai_score=ai_score)
