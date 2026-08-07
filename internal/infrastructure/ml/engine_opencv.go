//go:build opencv

package ml

import (
	"context"
	"fmt"
	"image"

	"gocv.io/x/gocv"

	"github.com/Lmex89/home-cameras/internal/config"
	"github.com/Lmex89/home-cameras/internal/domain"
)

// gocvDetector runs YOLO inference through OpenCV's DNN module using an
// ONNX-exported model. Export once from Python (see scripts/export_yolo_onnx.py):
//
//	from ultralytics import YOLO
//	YOLO("yolov8n.pt").export(format="onnx", imgsz=640)
//
// The engine expects the default ultralytics export layout (transpose
// disabled): a 1x84x8400 tensor in NCHW order, where each of the 8400
// anchor positions carries cx, cy, w, h plus 80 class scores.
type gocvDetector struct {
	net       gocv.Net
	threshold float32
	inputSize int
	classes   map[int]string
}

// yoloClasses are the COCO class names for the default YOLOv8 model.
var yoloClasses = []string{
	"person", "bicycle", "car", "motorcycle", "airplane", "bus", "train",
	"truck", "boat", "traffic light", "fire hydrant", "stop sign",
	"parking meter", "bench", "bird", "cat", "dog", "horse", "sheep",
	"cow", "elephant", "bear", "zebra", "giraffe", "backpack", "umbrella",
	"handbag", "tie", "suitcase", "frisbee", "skis", "snowboard",
	"sports ball", "kite", "baseball bat", "baseball glove", "skateboard",
	"surfboard", "tennis racket", "bottle", "wine glass", "cup", "fork",
	"knife", "spoon", "bowl", "banana", "apple", "sandwich", "orange",
	"broccoli", "carrot", "hot dog", "pizza", "donut", "cake", "chair",
	"couch", "potted plant", "bed", "dining table", "toilet", "tv",
	"laptop", "mouse", "remote", "keyboard", "cell phone", "microwave",
	"oven", "toaster", "sink", "refrigerator", "book", "clock", "vase",
	"scissors", "teddy bear", "hair drier", "toothbrush",
}

const (
	numClasses   = 80
	numAnchors   = 8400
	boxFields    = 4
	classOffset  = boxFields
	outputFields = boxFields + numClasses // 84
)

// newGOCVEngine loads the ONNX YOLO model; falls back to the stub when
// the model file is missing or loading fails.
func newGOCVEngine(cfg config.Config) Detector {
	modelPath := ModelPath(cfg)
	if modelPath == "" {
		return stubDetector{}
	}
	net := gocv.ReadNet(modelPath, "")
	if net.Empty() {
		return stubDetector{}
	}
	classes := make(map[int]string, len(yoloClasses))
	for i, name := range yoloClasses {
		classes[i] = name
	}
	return &gocvDetector{
		net:       net,
		threshold: float32(cfg.YoloConfidenceThreshold),
		inputSize: 640,
		classes:   classes,
	}
}

// Available reports whether the model is loaded.
func (d *gocvDetector) Available() bool { return !d.net.Empty() }

// Detect runs ONNX inference and parses YOLOv8 outputs.
func (d *gocvDetector) Detect(ctx context.Context, imagePath string) ([]domain.Detection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	img := gocv.IMRead(imagePath, gocv.IMReadColor)
	if img.Empty() {
		return nil, fmt.Errorf("cannot read image %s", imagePath)
	}
	defer img.Close()

	blob := gocv.BlobFromImage(img, 1.0/255.0,
		image.Pt(d.inputSize, d.inputSize),
		gocv.NewScalar(0, 0, 0, 0), true, false)
	defer blob.Close()

	d.net.SetInput(blob, "")
	output := d.net.Forward("")
	defer output.Close()

	total := int(output.Total())
	if total != numAnchors*outputFields {
		return nil, fmt.Errorf("unexpected YOLO output size %d (want %d)", total, numAnchors*outputFields)
	}
	// Flatten the NCHW [1,84,8400] tensor into a single row so the
	// memory layout (channel-major) can be indexed directly.
	flat := output.Reshape(1, total)
	defer flat.Close()

	detections := make([]domain.Detection, 0, 16)
	for col := 0; col < numAnchors; col++ {
		bestCls, bestScore := -1, float32(0)
		for cls := 0; cls < numClasses; cls++ {
			score := flat.GetFloatAt(0, (classOffset+cls)*numAnchors+col)
			if score > bestScore {
				bestScore, bestCls = score, cls
			}
		}
		if bestCls < 0 || bestScore < d.threshold {
			continue
		}
		cx := flat.GetFloatAt(0, 0*numAnchors+col)
		cy := flat.GetFloatAt(0, 1*numAnchors+col)
		w := flat.GetFloatAt(0, 2*numAnchors+col)
		h := flat.GetFloatAt(0, 3*numAnchors+col)
		detections = append(detections, domain.Detection{
			ClassName:  d.classes[bestCls],
			Confidence: float64(bestScore),
			BBox: []float64{
				float64(cx - w/2), float64(cy - h/2),
				float64(cx + w/2), float64(cy + h/2),
			},
		})
	}
	return detections, nil
}
