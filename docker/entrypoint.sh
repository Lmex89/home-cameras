#!/bin/sh
# Container entrypoint: bootstrap the cameras seed file and warn when the
# configured YOLO model is missing (the detector then runs in stub mode).
set -eu

if [ ! -e /cameras.yaml ]; then
    echo "[entrypoint] /cameras.yaml not found — seeding from cameras.example.yaml"
    cp /cameras.example.yaml /cameras.yaml
fi

model="${YOLO_MODEL_PATH:-models/yolov8n.onnx}"
if [ -n "$model" ] && [ ! -f "$model" ] && [ ! -f "${model%.*}.onnx" ]; then
    echo "[entrypoint] WARNING: YOLO model not found at $model — object detection will run in stub mode"
    echo "[entrypoint]          mount a local models/ dir (see docker-compose.yml) or use the opencv overlay"
fi

exec /cameras
