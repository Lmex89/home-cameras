# Build tooling for the Go port of the camera monitor.

BINARY  := bin/cameras-go
PKG     := ./cmd/server

.PHONY: build run dev test vet fmt docker clean

## build: compile the server binary into bin/
build:
	go build -o $(BINARY) $(PKG)

## run: build and launch against the local ./data directory
run: build
	./$(BINARY)

## dev: run with air (live reload, install with: go install github.com/air-verse/air@latest)
dev:
	air

## test: run all tests with the race detector
test:
	go test ./... -race -cover

## vet: static analysis
vet:
	go vet ./...

## fmt: format all sources
fmt:
	gofmt -w cmd internal

## opencv: build with the native gocv YOLO engine (requires OpenCV dev headers)
opencv:
	CGO_ENABLED=1 go build -tags opencv -o $(BINARY) $(PKG)

## docker: build the container image
docker:
	docker build -t cameras-go .

## clean: remove build artifacts
clean:
	rm -rf bin
