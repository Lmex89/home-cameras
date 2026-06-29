#!/usr/bin/env fish

# Restart the uvicorn development server on port 8002.
#
# Kills any existing uvicorn process, then starts a new one in the
# background via nohup so it keeps running after the terminal closes.
#
# Usage:
#     fish restart.fish

set venv_python "$PWD/.venv/bin/python"

if not test -x $venv_python
    echo "Virtualenv python not found at $venv_python" >&2
    exit 1
end

echo "Stopping existing uvicorn processes..."
pkill -f "uvicorn app.main" 2>/dev/null
sleep 1

echo "Starting uvicorn on 0.0.0.0:8002..."
nohup $venv_python -m uvicorn app.main:app --reload --port=8002 --host 0.0.0.0 > /tmp/uvicorn.log 2>&1 &
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
