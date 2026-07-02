#!/usr/bin/env fish

# Restart the uvicorn development server on port 8002.
#
# Kills any existing uvicorn process, then starts a new one in the
# background via nohup so it keeps running after the terminal closes.
# Downloads a YOLO model if missing for ML analysis.
#
# Usage:
#     fish restart.fish

set venv_python "$PWD/.venv/bin/python"
set model_dir "$PWD/models"
set yolo_model "$model_dir/yolov8n.pt"

if not test -x $venv_python
    echo "Virtualenv python not found at $venv_python" >&2
    exit 1
end

# Ensure model directory exists
mkdir -p $model_dir

# Download YOLO model if missing
if not test -f $yolo_model
    echo "YOLO model not found at $yolo_model — downloading..."
    curl -sL -o $yolo_model \
        "https://github.com/ultralytics/assets/releases/download/v8.2.0/yolov8n.pt"
    if test $status -eq 0
        echo "Downloaded yolov8n.pt"
    else
        echo "Warning: download failed — ML pipeline will run in stub mode" >&2
    end
end

echo "Stopping existing uvicorn processes..."
pkill -f "uvicorn app.main:app" 2>/dev/null
# Wait for port 8002 to be freed (up to 5 seconds)
for i in (seq 1 5)
    if not ss -tlnp 2>/dev/null | grep -q ":8002 "
        echo "Port 8002 is free"
        break
    end
    echo "Waiting for port 8002... ($i/5)"
    sleep 1
end

echo "Starting uvicorn on 0.0.0.0:8002..."
set -lx ANALYSIS_ENABLED true
set -lx YOLO_MODEL_PATH $yolo_model
nohup $venv_python -m uvicorn app.main:app --port=8002 --host 0.0.0.0 > /tmp/uvicorn.log 2>&1 &
set pid $last_pid

echo "Started uvicorn PID $pid"
echo "Logs: tail -f /tmp/uvicorn.log"

# Generate dashboard manifest
echo "Generating dashboard manifest..."
$venv_python "$PWD/tools/export_manifest.py"

echo ""
echo "Dashboard URLs:"
echo "  FastAPI dashboard: http://<this-ip>:8002/"
echo "  Static dashboard:  http://<this-ip>:8002/index.html"
