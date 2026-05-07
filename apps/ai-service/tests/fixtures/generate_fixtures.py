#!/usr/bin/env python3
"""Generate minimal ONNX fixture models for AI service integration tests.

Run once to regenerate the committed binaries:
    python tests/fixtures/generate_fixtures.py

The produced files are committed so CI does not need the 'onnx' build package —
only onnxruntime (ml extras) is required to load and run them.

SAM fixture  — input 'image' (1,3,1024,1024), outputs 'masks' (1,1,1,1) + 'iou_predictions' (1,1)
YOLO fixture — input 'images' (1,3,640,640), output 'output0' (1,2,6)
"""

from __future__ import annotations

from pathlib import Path

import numpy as np
import onnx
from onnx import helper, TensorProto
from onnx import numpy_helper


_HERE = Path(__file__).parent


def _make_sam_fixture(output_path: Path) -> None:
    image_input = helper.make_tensor_value_info(
        "image", TensorProto.FLOAT, [1, 3, 1024, 1024]
    )
    masks_output = helper.make_tensor_value_info(
        "masks", TensorProto.FLOAT, [1, 1, 1, 1]
    )
    iou_output = helper.make_tensor_value_info(
        "iou_predictions", TensorProto.FLOAT, [1, 1]
    )

    masks_node = helper.make_node(
        "Constant",
        inputs=[],
        outputs=["masks"],
        value=numpy_helper.from_array(
            np.zeros((1, 1, 1, 1), dtype=np.float32), name="masks_const"
        ),
    )
    iou_node = helper.make_node(
        "Constant",
        inputs=[],
        outputs=["iou_predictions"],
        value=numpy_helper.from_array(
            np.array([[0.9]], dtype=np.float32), name="iou_const"
        ),
    )

    graph = helper.make_graph(
        [masks_node, iou_node],
        "sam_fixture",
        [image_input],
        [masks_output, iou_output],
    )
    model = helper.make_model(graph, opset_imports=[helper.make_opsetid("", 17)])
    model.ir_version = 8
    onnx.save(model, str(output_path))
    print(f"  wrote {output_path} ({output_path.stat().st_size} bytes)")


def _make_yolo_fixture(output_path: Path) -> None:
    images_input = helper.make_tensor_value_info(
        "images", TensorProto.FLOAT, [1, 3, 640, 640]
    )
    output0 = helper.make_tensor_value_info(
        "output0", TensorProto.FLOAT, [1, 2, 6]
    )

    dets = np.array(
        [[[10.0, 20.0, 100.0, 200.0, 0.9, 0.0],
          [15.0, 25.0,  90.0, 190.0, 0.8, 1.0]]],
        dtype=np.float32,
    )
    det_node = helper.make_node(
        "Constant",
        inputs=[],
        outputs=["output0"],
        value=numpy_helper.from_array(dets, name="output0_const"),
    )

    graph = helper.make_graph(
        [det_node],
        "yolo_fixture",
        [images_input],
        [output0],
    )
    model = helper.make_model(graph, opset_imports=[helper.make_opsetid("", 17)])
    model.ir_version = 8
    onnx.save(model, str(output_path))
    print(f"  wrote {output_path} ({output_path.stat().st_size} bytes)")


def main() -> None:
    print("Generating ONNX test fixtures ...")
    _make_sam_fixture(_HERE / "sam_fixture.onnx")
    _make_yolo_fixture(_HERE / "yolo_fixture.onnx")
    print("Done.")


if __name__ == "__main__":
    main()
