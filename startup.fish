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
#     1. Sources shared functions from lib/shared.fish.
#     2. Resolves the project directory via realpath (works from any CWD).
#     3. Validates the virtualenv Python interpreter exists.
#     4. Ensures the models/ directory exists.
#     5. Downloads yolov8n.pt from GitHub Releases if not present.
#     6. Exports ANALYSIS_ENABLED=true and YOLO_MODEL_PATH env vars.
#     7. Runs tools/export_manifest.py to generate the dashboard manifest.
#     8. Execs into uvicorn (replaces this shell process).
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
source "$script_dir/lib/shared.fish"

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

ensure_python $venv_python
ensure_model_dir $model_dir
download_yolo_model $yolo_model
set_analysis_env $yolo_model
generate_manifest $venv_python $script_dir

# Restart mode: kill existing process and wait for port to free
if test "$do_restart" = true
    kill_uvicorn
    wait_for_port 8002 5
end

# Exec into uvicorn — replaces this shell process so systemd tracks uvicorn directly
echo "Starting uvicorn on 0.0.0.0:8002..."
exec $venv_python -m uvicorn app.main:app --port 8002 --host 0.0.0.0
