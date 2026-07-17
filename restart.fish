#!/usr/bin/env fish

# Restart the uvicorn development server on port 8002.
#
# Kills any existing uvicorn process, then starts a new one in the
# background via nohup so it keeps running after the terminal closes.
# Downloads a YOLO model if missing for ML analysis.
#
# Usage:
#     fish restart.fish
#
# Behavior:
#     1. Sources shared functions from lib/shared.fish.
#     2. Validates the virtualenv Python interpreter exists.
#     3. Ensures the models/ directory exists.
#     4. Downloads yolov8n.pt from GitHub Releases if not present.
#     5. Kills any running "uvicorn app.main:app" process.
#     6. Waits up to 5 seconds for port 8002 to become free.
#     7. Sets ANALYSIS_ENABLED=true and YOLO_MODEL_PATH env vars.
#     8. Starts uvicorn in the background via nohup, logging to /tmp/uvicorn.log.
#     9. Runs tools/export_manifest.py to generate the dashboard manifest.
#    10. Prints the PID and dashboard URLs.
#
# Notes:
#     - This script is for manual/interactive use. For systemd auto-start,
#       use startup.fish instead.
#     - The background process survives terminal close but will NOT restart
#       on crash. Use the systemd service for production.

source "$PWD/lib/shared.fish"

set venv_python "$PWD/.venv/bin/python"
set model_dir "$PWD/models"
set yolo_model "$model_dir/yolov8n.pt"

ensure_python $venv_python
ensure_model_dir $model_dir
download_yolo_model $yolo_model

kill_uvicorn
wait_for_port 8002 5

echo "Starting uvicorn on 0.0.0.0:8002..."
nohup $venv_python -m uvicorn app.main:app --port=8002 --host 0.0.0.0 > /tmp/uvicorn.log 2>&1 &
set pid $last_pid

echo "Started uvicorn PID $pid"
echo "Logs: tail -f /tmp/uvicorn.log"

generate_manifest $venv_python $PWD

echo ""
echo "Dashboard URLs:"
echo "  FastAPI dashboard: http://<this-ip>:8002/"
echo "  Static dashboard:  http://<this-ip>:8002/index.html"
