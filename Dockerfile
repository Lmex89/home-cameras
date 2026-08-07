# Multi-stage build for the Go camera monitor.
# The default image is pure Go (no CGO, no OpenCV) — the object
# detector runs in stub mode. For native YOLO inference build with:
#   docker build --build-arg BUILD_TAGS=opencv --build-arg BASE=opencv .
ARG BASE=alpine

FROM golang:1.26-alpine AS builder
ARG BUILD_TAGS=""
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -tags "${BUILD_TAGS}" -ldflags="-s -w" -o /cameras ./cmd/server/

# Runtime image: scratch for the default build (single static binary).
FROM scratch AS runtime-default
COPY --from=builder /cameras /cameras
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
ENV DATA_DIR=/data
VOLUME ["/data"]
EXPOSE 8004
ENTRYPOINT ["/cameras"]

# Runtime image: alpine with ffmpeg for RTSP fallback captures and
# timelapse rendering (the legacy Docker image also ships ffmpeg).
FROM alpine AS runtime-opencv
RUN apk add --no-cache ffmpeg ca-certificates
COPY --from=builder /cameras /cameras
ENV DATA_DIR=/data
VOLUME ["/data"]
EXPOSE 8004
ENTRYPOINT ["/cameras"]

FROM runtime-${BASE}
