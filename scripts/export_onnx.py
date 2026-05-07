#!/usr/bin/env python3
"""Export SAM 2.1 and YOLOv11x models to ONNX for CPU inference fallback.

Usage:
    python scripts/export_onnx.py --model sam --weights /path/to/sam2.1_hiera_large.pt \
        --output-dir /models
    python scripts/export_onnx.py --model yolo --weights /path/to/yolov11x.pt \
        --output-dir /models
    python scripts/export_onnx.py --model all --weights /path/to/weights_dir \
        --output-dir /models

Spec §5: SAM 2.1 and YOLOv11x deployed as ONNX for CPU inference fallback.
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path


def export_sam(weights_path: Path, output_dir: Path) -> Path:
    """Export SAM 2.1 to a single end-to-end ONNX session.

    Uses torch.onnx.export with a fixed 1024×1024 image input shape.
    Validates the exported model with onnxruntime before returning.
    """
    try:
        import torch
        import onnxruntime as ort
    except ImportError as exc:
        raise SystemExit(
            f"Missing dependency: {exc}. Install with: pip install -e '.[ml]' torch"
        ) from exc

    output_path = output_dir / "sam2.1_hiera_large.onnx"
    output_dir.mkdir(parents=True, exist_ok=True)

    print(f"Loading SAM 2.1 weights from {weights_path} ...")
    # SAM 2.1 export: wrap model so torch.onnx sees a single forward pass.
    # The exported model takes a 1×3×1024×1024 image tensor and returns
    # masks (1×N×1024×1024) and iou_predictions (1×N).
    try:
        from sam2.build_sam import build_sam2  # type: ignore[import]
        from sam2.sam2_image_predictor import SAM2ImagePredictor  # type: ignore[import]
    except ImportError as exc:
        raise SystemExit(
            f"SAM 2 package not found: {exc}. "
            "Install from https://github.com/facebookresearch/segment-anything-2"
        ) from exc

    model = build_sam2("sam2_hiera_l.yaml", str(weights_path))
    model.eval()

    dummy = torch.zeros(1, 3, 1024, 1024)
    print(f"Exporting to {output_path} ...")
    torch.onnx.export(
        model,
        dummy,
        str(output_path),
        input_names=["image"],
        output_names=["masks", "iou_predictions"],
        dynamic_axes={"image": {0: "batch"}},
        opset_version=17,
    )

    _validate_onnx(output_path, providers=["CPUExecutionProvider"])
    print(f"SAM 2.1 exported and validated → {output_path}")
    return output_path


def export_yolo(weights_path: Path, output_dir: Path) -> Path:
    """Export YOLOv11x to ONNX using Ultralytics built-in export."""
    try:
        from ultralytics import YOLO  # type: ignore[import]
        import onnxruntime as ort  # noqa: F401
    except ImportError as exc:
        raise SystemExit(
            f"Missing dependency: {exc}. Install with: pip install ultralytics"
        ) from exc

    output_dir.mkdir(parents=True, exist_ok=True)
    print(f"Loading YOLOv11x weights from {weights_path} ...")
    model = YOLO(str(weights_path))

    print("Exporting YOLOv11x to ONNX ...")
    exported = model.export(format="onnx", imgsz=640, opset=17, simplify=True)
    output_path = Path(exported)

    if output_path.parent != output_dir:
        dest = output_dir / output_path.name
        output_path.rename(dest)
        output_path = dest

    _validate_onnx(output_path, providers=["CPUExecutionProvider"])
    print(f"YOLOv11x exported and validated → {output_path}")
    return output_path


def _validate_onnx(path: Path, providers: list[str]) -> None:
    """Load the exported model with onnxruntime; raise on any error."""
    import onnxruntime as ort

    session = ort.InferenceSession(str(path), providers=providers)
    inputs = [i.name for i in session.get_inputs()]
    outputs = [o.name for o in session.get_outputs()]
    print(f"  inputs : {inputs}")
    print(f"  outputs: {outputs}")


def main(argv: list[str] | None = None) -> None:
    parser = argparse.ArgumentParser(description="Export models to ONNX.")
    parser.add_argument(
        "--model",
        choices=["sam", "yolo", "all"],
        required=True,
        help="Which model to export.",
    )
    parser.add_argument(
        "--weights",
        type=Path,
        required=True,
        help="Path to PyTorch weights file (or directory when --model all).",
    )
    parser.add_argument(
        "--output-dir",
        type=Path,
        default=Path("/models"),
        help="Directory to write .onnx files into (default: /models).",
    )
    args = parser.parse_args(argv)

    if args.model == "sam":
        export_sam(args.weights, args.output_dir)
    elif args.model == "yolo":
        export_yolo(args.weights, args.output_dir)
    else:
        export_sam(args.weights / "sam2.1_hiera_large.pt", args.output_dir)
        export_yolo(args.weights / "yolov11x.pt", args.output_dir)


if __name__ == "__main__":
    main()
