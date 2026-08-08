# Multi-stage build for the Go camera monitor.
#
# Default build: pure-Go stub detector + alpine runtime with ffmpeg.
#   docker compose up -d --build
#
# Native gocv YOLO inference (bakes an opset-11 ONNX export at
# /models/yolov8n.onnx — no host-side `make model` needed):
#   docker compose -f docker-compose.yml -f docker-compose.opencv.yml up -d --build
#   (or: docker build --build-arg BUILD_TAGS=opencv --build-arg BASE=alpine-opencv .)

ARG BASE=alpine
ARG BUILD_TAGS=""

FROM golang:1.26-alpine AS builder
ARG BUILD_TAGS=""
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN if [ -n "$BUILD_TAGS" ]; then \
      apk add --no-cache build-base pkgconf opencv-dev; \
      CGO_ENABLED=1 go build -tags "${BUILD_TAGS}" -ldflags="-s -w" -o /cameras ./cmd/server/; \
    else \
      CGO_ENABLED=0 go build -ldflags="-s -w" -o /cameras ./cmd/server/; \
    fi

# Build-time YOLO export stage (glibc python, opset 11 for OpenCV < 4.8).
# Only pulled into the opencv runtime image, so stub builds never pay
# for the torch download. Kept at build time (not baked at runtime) so
# the container stays small and the image layers get cached.
FROM python:3.13-slim AS model-builder
ARG MODEL_URL=https://github.com/ultralytics/assets/releases/download/v8.4.0/yolov8n.pt
WORKDIR /model
# The export only needs ultralytics' model-conversion code paths. Replace
# the GUI opencv-python (its import requires X11 libs like libxcb/libGL,
# missing on slim images) with the headless build — no apt/libX needed.
RUN pip install --no-cache-dir -q torch --index-url https://download.pytorch.org/whl/cpu \
    && pip install --no-cache-dir -q ultralytics onnxslim \
    && pip uninstall -y -q opencv-python \
    && pip install --no-cache-dir -q opencv-python-headless
COPY scripts/export_yolo_onnx.py /export.py
RUN MODEL_URL="$MODEL_URL" python -c 'import os; from urllib.request import urlretrieve; urlretrieve(os.environ["MODEL_URL"], "yolov8n.pt")' \
    && python /export.py yolov8n.pt --out . --opset 11

# Runtime base: alpine with ffmpeg (RTSP fallback captures + timelapse
# rendering), CA certs and tzdata. The entrypoint seeds cameras.yaml from
# the example file on first start and warns about a missing YOLO model.
FROM alpine AS runtime-alpine
RUN apk add --no-cache ffmpeg ca-certificates tzdata
COPY --from=builder /cameras /cameras
COPY docker/entrypoint.sh /entrypoint.sh
COPY cameras.example.yaml /cameras.example.yaml
RUN chmod +x /entrypoint.sh
ENV DATA_DIR=/data
VOLUME ["/data"]
EXPOSE 8004
ENTRYPOINT ["/entrypoint.sh"]

# Opencv runtime: adds OpenCV shared libraries and the baked YOLO model.
# Alpine splits OpenCV into per-module packages; the base `opencv` package
# only ships core/imgproc/dnn/..., so install every module the gocv binary
# links against (photo, video, ml, shape, stitching, tracking, ...).
FROM runtime-alpine AS runtime-alpine-opencv
RUN apk add --no-cache opencv \
    libopencv_aruco libopencv_face libopencv_ml libopencv_optflow \
    libopencv_photo libopencv_plot libopencv_shape libopencv_stitching \
    libopencv_superres libopencv_tracking libopencv_video
COPY --from=model-builder /model/yolov8n.onnx /models/yolov8n.onnx
ENV YOLO_MODEL_PATH=models/yolov8n.onnx

# Backward-compatible alias so `BASE=opencv` keeps working.
FROM runtime-alpine-opencv AS runtime-opencv

FROM runtime-${BASE}
