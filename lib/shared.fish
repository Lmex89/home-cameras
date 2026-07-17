#!/usr/bin/env fish

# Shared setup functions for fish scripts.
#
# Sourced by startup.fish and restart.fish to avoid code duplication.
# Provides virtualenv validation, YOLO model download, process management,
# environment setup, and manifest generation.

function ensure_python -a python_path
    if not test -x $python_path
        echo "ERROR: Virtualenv python not found at $python_path" >&2
        exit 1
    end
end

function ensure_model_dir -a model_dir
    mkdir -p $model_dir
end

function download_yolo_model -a model_path
    if not test -f $model_path
        echo "YOLO model not found at $model_path — downloading..."
        if curl -sL -o $model_path \
            "https://github.com/ultralytics/assets/releases/download/v8.2.0/yolov8n.pt"
            echo "Downloaded yolov8n.pt"
        else
            echo "Warning: download failed — ML pipeline will run in stub mode" >&2
        end
    end
end

function set_analysis_env -a model_path
    set -gx ANALYSIS_ENABLED true
    set -gx YOLO_MODEL_PATH $model_path
end

function kill_uvicorn
    echo "Stopping existing uvicorn processes..."
    pkill -f "uvicorn app.main:app" 2>/dev/null
end

function wait_for_port -a port max_wait
    for i in (seq 1 $max_wait)
        if not ss -tlnp 2>/dev/null | grep -q ":$port "
            echo "Port $port is free"
            return 0
        end
        echo "Waiting for port $port... ($i/$max_wait)"
        sleep 1
    end
    echo "Warning: port $port may still be in use" >&2
end

function generate_manifest -a python_path base_dir
    echo "Generating dashboard manifest..."
    if test -f "$base_dir/tools/export_manifest.py"
        $python_path "$base_dir/tools/export_manifest.py"
    else
        echo "Warning: export_manifest.py not found, skipping manifest generation" >&2
    end
end
