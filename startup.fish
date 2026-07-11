#!/usr/bin/env fish

# Startup script for Camera Monitor - ONVIF Snapshot Service.
#
# Prepares the environment (downloads YOLO model if missing, generates
# dashboard manifest) then execs into uvicorn so systemd can track the
# process lifecycle.
#
# Usage:
#     fish startup.fish              # Normal start (systemd)
#     fish startup.fish --restart    # Kill existing process, then start (development)
#
# Behavior:
#     1. Resolves the project directory via realpath (works from any CWD).
#     2. Validates the virtualenv Python interpreter exists.
#     3. Ensures the models/ directory exists.
#     4. Downloads yolov8n.pt from GitHub Releases if not present.
#     5. Exports ANALYSIS_ENABLED=true and YOLO_MODEL_PATH env vars.
#     6. Runs tools/export_manifest.py to generate the dashboard manifest.
#     7. Execs into uvicorn (replaces this shell process).
#
# --restart flag:
#     - Kills any running "uvicorn app.main:app" process.
#     - Waits up to 5 seconds for port 8002 to become free.
#     - Useful during development when iterating on code changes.
#
# Notes:
#     - The exec call replaces the fish process with uvicorn, so systemd
#       receives the correct PID and can manage restarts.
#     - For a full restart with status output, use restart.fish instead.

set script_dir (dirname (realpath (status -f)))
set venv_python "$script_dir/.venv/bin/python"
set model_dir "$script_dir/models"
set yolo_model "$model_dir/yolov8n.pt"

# Parse flags
set do_restart false
for arg in $argv
    if test "$arg" = "--restart"
        set do_restart true
    end
end

# Validate virtualenv
if not test -x $venv_python
    echo "ERROR: Virtualenv python not found at $venv_python" >&2
    exit 1
end

# Ensure model directory exists
mkdir -p $model_dir

# Download YOLO model if missing
if not test -f $yolo_model
    echo "YOLO model not found at $yolo_model — downloading..."
    if curl -sL -o $yolo_model \
        "https://github.com/ultralytics/assets/releases/download/v8.2.0/yolov8n.pt"
        echo "Downloaded yolov8n.pt"
    else
        echo "Warning: download failed — ML pipeline will run in stub mode" >&2
    end
end

# Set environment variables (same as restart.fish)
set -gx ANALYSIS_ENABLED true
set -gx YOLO_MODEL_PATH $yolo_model

# Generate dashboard manifest
echo "Generating dashboard manifest..."
if test -f "$script_dir/tools/export_manifest.py"
    $venv_python "$script_dir/tools/export_manifest.py"
else
    echo "Warning: export_manifest.py not found, skipping manifest generation" >&2
end

# Restart mode: kill existing process and wait for port to free
if test "$do_restart" = true
    echo "Stopping existing uvicorn processes..."
    pkill -f "uvicorn app.main:app" 2>/dev/null

    for i in (seq 1 5)
        if not ss -tlnp 2>/dev/null | grep -q ":8002 "
            echo "Port 8002 is free"
            break
        end
        echo "Waiting for port 8002... ($i/5)"
        sleep 1
    end
end

# Exec into uvicorn — replaces this shell process so systemd tracks uvicorn directly
echo "Starting uvicorn on 0.0.0.0:8002..."
exec $venv_python -m uvicorn app.main:app --port 8002 --host 0.0.0.0
