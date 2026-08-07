#!/usr/bin/env python3
"""Export a YOLOv8 weights file to ONNX for the Go inference engine.

Run once from the Python venv to produce the model consumed by the
gocv-based detector (internal/infrastructure/ml/engine_opencv.go):

    python scripts/export_yolo_onnx.py [model_path] [output_dir]

The exporter keeps the default ultralytics layout (transpose disabled)
so the output tensor is 1x84x8400 in NCHW order, which the Go parser
expects. The resulting file should be placed next to the .pt weights
(e.g. models/yolov8n.onnx) or the path set via YOLO_MODEL_PATH.

Default opset is 11: OpenCV < 4.8 (e.g. Ubuntu 22.04's 4.5.4) cannot
parse the opset 17+ exports newer ultralytics produces by default and
aborts at inference time. Raise --opset if your OpenCV supports it.
"""

import argparse
import sys
from pathlib import Path


def main() -> int:
    """Parse arguments and run the ONNX export."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "model",
        nargs="?",
        default="models/yolov8n.pt",
        help="Path to the YOLO weights file (default: models/yolov8n.pt)",
    )
    parser.add_argument(
        "--out",
        default=None,
        help="Output directory (default: same directory as the weights)",
    )
    parser.add_argument("--imgsz", type=int, default=640, help="Input size (default: 640)")
    parser.add_argument("--opset", type=int, default=11, help="ONNX opset (default: 11)")
    parser.add_argument(
        "--simplify",
        action=argparse.BooleanOptionalAction,
        default=True,
        help="Constant-fold the graph with onnxslim (default: enabled)",
    )
    args = parser.parse_args()

    try:
        from ultralytics import YOLO
    except ImportError as exc:
        print(f"ultralytics is not installed: {exc}", file=sys.stderr)
        return 1

    model_path = Path(args.model)
    if not model_path.exists():
        print(f"Model not found: {model_path}", file=sys.stderr)
        return 1

    out_dir = Path(args.out) if args.out else model_path.parent
    out_dir.mkdir(parents=True, exist_ok=True)

    model = YOLO(str(model_path))
    # transpose stays False (default) so the Go parser can index the
    # NCHW 1x84x8400 tensor directly.
    exported = model.export(
        format="onnx", imgsz=args.imgsz, half=False,
        opset=args.opset, simplify=args.simplify,
    )
    print(f"Exported ONNX model: {exported}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
