# Build tooling for the Go port of the camera monitor.

BINARY     := bin/cameras-go
PKG        := ./cmd/server
MODEL_DIR  := models
MODEL_FILE := $(MODEL_DIR)/yolov8n.onnx
MODEL_PT   := $(MODEL_DIR)/yolov8n.pt
MODEL_URL  := https://github.com/ultralytics/assets/releases/download/v8.4.0/yolov8n.pt
VENV       := .venv
OPENCV_PREFIX ?= $(HOME)/.local/opencv
OPENCV_VERSION := 4.10.0
OPENCV_URL  := https://github.com/opencv/opencv/archive/refs/tags/$(OPENCV_VERSION).tar.gz
# Cap the OpenCV parallel build at 12 cores to avoid OOM on smaller machines.
OPENCV_JOBS := $(shell n=$$(nproc); [ "$$n" -gt 12 ] && echo 12 || echo "$$n")

# Prefer a local OpenCV >= 4.8 (YOLOv8 needs it) over the distro's 4.5.4
# when one has been installed via `make opencv-build`.
PKG_CONFIG_PATH := $(OPENCV_PREFIX)/lib/pkgconfig:$(PKG_CONFIG_PATH)
LD_LIBRARY_PATH := $(OPENCV_PREFIX)/lib:$(LD_LIBRARY_PATH)
export PKG_CONFIG_PATH LD_LIBRARY_PATH

.PHONY: build run dev test vet fmt docker docker-up docker-down docker-build-opencv clean requirements model model-force clean-model opencv opencv-build frontend frontend-up frontend-dev

## build: compile the server binary into bin/
build:
	go build -o $(BINARY) $(PKG)

## run: check requirements, build (gocv engine when OpenCV >= 4.8 is present) and launch
run: requirements
	@if pkg-config --atleast-version=4.8 opencv4; then \
		$(MAKE) --no-print-directory opencv; \
	else \
		echo "WARN: OpenCV >= 4.8 not found — building the stub detector."; \
		echo "      Build a local one with 'make opencv-build' to enable real YOLO."; \
		$(MAKE) --no-print-directory build; \
	fi
	./$(BINARY)

## dev: run with air (live reload, install with: go install github.com/air-verse/air@latest)
dev: requirements
	air

## requirements: make sure runtime deps (YOLO model, ffmpeg, OpenCV) are in place
requirements: model
	@command -v ffmpeg >/dev/null 2>&1 \
		&& echo "OK: ffmpeg found (RTSP fallback + timelapse)" \
		|| echo "WARN: ffmpeg not on PATH — RTSP fallback capture and timelapse videos need it"
	@if pkg-config --atleast-version=4.8 opencv4; then \
		echo "OK: OpenCV $(shell pkg-config --modversion opencv4) found (gocv YOLO engine available)"; \
	elif pkg-config --exists opencv4; then \
		echo ""; \
		echo "WARN: OpenCV $(shell pkg-config --modversion opencv4) is too old — YOLOv8 ONNX aborts on OpenCV < 4.8."; \
		echo "      Build a local one (no sudo): make opencv-build"; \
		echo "      then rebuild with:            make opencv"; \
	else \
		echo ""; \
		echo "WARN: OpenCV dev headers not found — YOLO detection will run in stub mode."; \
		echo "      The distro package (libopencv-dev) is 4.5.4, too old for YOLOv8."; \
		echo "      Build a local one (no sudo): make opencv-build"; \
		echo "      then rebuild with:            make opencv"; \
	fi

## model: produce the YOLOv8 ONNX engine file (opset 11) from the .pt weights
## The raw ultralytics ONNX assets are opset 17+ and crash OpenCV < 4.8
## at inference, so we download the .pt once and export it locally.
model: $(MODEL_FILE)

$(MODEL_FILE): $(MODEL_PT) scripts/export_yolo_onnx.py
	@if [ ! -x "$(VENV)/bin/python" ]; then python3 -m venv $(VENV); fi
	@$(VENV)/bin/pip install -q torch --index-url https://download.pytorch.org/whl/cpu
	@$(VENV)/bin/pip install -q ultralytics
	@$(VENV)/bin/python scripts/export_yolo_onnx.py $(MODEL_PT) --out $(MODEL_DIR) --opset 11
	@echo "OK: exported $(MODEL_FILE)"

$(MODEL_PT):
	@mkdir -p $(MODEL_DIR)
	curl -fL -o $@ $(MODEL_URL) || { rm -f $@; echo "ERROR: model download failed"; exit 1; }
	@echo "OK: downloaded $@"

## model-force: re-download the weights and re-export the ONNX model
model-force: clean-model $(MODEL_FILE)

clean-model:
	@rm -f $(MODEL_PT) $(MODEL_FILE)
	@echo "removed $(MODEL_PT) $(MODEL_FILE)"

## test: run all tests with the race detector
test:
	go test ./... -race -cover

## vet: static analysis
vet:
	go vet ./...

## fmt: format all sources
fmt:
	gofmt -w cmd internal

## opencv: build with the native gocv YOLO engine (requires OpenCV >= 4.8)
## PKG_CONFIG_PATH is forced inline (not just exported) because cgo's
## pkg-config invocation runs in its own shell that may not inherit the
## make-level export, and CGO_LDFLAGS embeds an rpath pointing at the local
## OpenCV install so the binary resolves 4.10.0 at runtime without needing
## LD_LIBRARY_PATH on every invocation.
opencv: requirements
	@PKG_CONFIG_PATH="$(OPENCV_PREFIX)/lib/pkgconfig:$$PKG_CONFIG_PATH" \
		pkg-config --atleast-version=4.8 opencv4 || { \
		echo "ERROR: OpenCV >= 4.8 required for YOLOv8 (found $$($(PKG_CONFIG_PATH)="$(OPENCV_PREFIX)/lib/pkgconfig:$$PKG_CONFIG_PATH" pkg-config --modversion opencv4 2>/dev/null || echo none))."; \
		echo "  Build a local one (no sudo): make opencv-build"; \
		exit 1; }
	PKG_CONFIG_PATH="$(OPENCV_PREFIX)/lib/pkgconfig:$$PKG_CONFIG_PATH" \
	CGO_ENABLED=1 \
	CGO_LDFLAGS="-Wl,-rpath,$(OPENCV_PREFIX)/lib -L$(OPENCV_PREFIX)/lib" \
	go build -tags opencv -o $(BINARY) $(PKG)

## opencv-build: compile OpenCV $(OPENCV_VERSION) from source into $(OPENCV_PREFIX)
## The distro's OpenCV (4.5.4 on Ubuntu 22.04) cannot run YOLOv8 ONNX graphs
## (shape_utils assertion, fixed in 4.8.0), so this installs a modern one to
## the user prefix — no sudo required. Takes 5-15 minutes; run once.
opencv-build:
	@command -v cmake >/dev/null 2>&1 || { echo "ERROR: cmake not found (apt install cmake)"; exit 1; }
	@command -v g++ >/dev/null 2>&1 || { echo "ERROR: g++ not found (apt install g++)"; exit 1; }
	@command -v pkg-config >/dev/null 2>&1 || { echo "ERROR: pkg-config not found"; exit 1; }
	@mkdir -p $(HOME)/.cache/opencv-$(OPENCV_VERSION)
	@cd $(HOME)/.cache/opencv-$(OPENCV_VERSION) && \
		if [ ! -d opencv-$(OPENCV_VERSION) ]; then \
			echo "Downloading OpenCV $(OPENCV_VERSION) ..."; \
			curl -fL -o opencv.tar.gz $(OPENCV_URL) || { echo "ERROR: download failed"; exit 1; }; \
			tar xzf opencv.tar.gz; \
		fi
	@mkdir -p $(HOME)/.cache/opencv-$(OPENCV_VERSION)/build-$(OPENCV_VERSION)
	@cd $(HOME)/.cache/opencv-$(OPENCV_VERSION)/build-$(OPENCV_VERSION) && \
		cmake ../opencv-$(OPENCV_VERSION) \
			-DCMAKE_BUILD_TYPE=Release \
			-DCMAKE_INSTALL_PREFIX=$(OPENCV_PREFIX) \
			-DBUILD_LIST=core,imgproc,imgcodecs,videoio,video,highgui,calib3d,features2d,objdetect,photo,ml,shape,stitching,dnn \
			-DBUILD_TESTS=OFF -DBUILD_EXAMPLES=OFF -DBUILD_PERF_TESTS=OFF \
			-DBUILD_opencv_python3=OFF -DBUILD_JAVA=OFF \
			-DOPENCV_GENERATE_PKGCONFIG=ON \
			-DWITH_GTK=OFF -DWITH_QT=OFF -DWITH_V4L=OFF -DWITH_OPENCL=OFF \
			-DWITH_FFMPEG=ON -DWITH_JPEG=ON -DWITH_PNG=ON && \
		make -j$(OPENCV_JOBS) && \
		make install
	@echo "OK: OpenCV $(OPENCV_VERSION) installed in $(OPENCV_PREFIX)"
	@echo "    Rebuild the binary with: make opencv"

## docker: build the default (stub) container image
docker:
	docker compose build

## docker-up: build and start the containers in the background (stub detector)
docker-up:
	docker compose up -d --build

## docker-down: stop and remove the container (data volume is kept)
docker-down:
	docker compose down

## docker-build-opencv: build the native gocv YOLO image (bakes the model)
docker-build-opencv:
	docker compose -f docker-compose.yml -f docker-compose.opencv.yml build

## clean: remove build artifacts
clean:
	rm -rf bin

## frontend: build the React frontend Docker image
frontend:
	docker build -t cameras-frontend:latest frontend

## frontend-up: build and start the frontend container (requires the cameras service running)
frontend-up:
	docker compose up -d --build frontend

## frontend-dev: run the Vite dev server (needs Node.js >= 18; proxies /api to :8004)
frontend-dev:
	cd frontend && npm install && npm run dev
