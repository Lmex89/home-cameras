#!/usr/bin/env fish

# Trigger the retention/archive cleanup job manually.
#
# Runs the same pipeline as the daily 03:00 cron: zips snapshots
# and videos older than the zip threshold, then deletes records
# and archives past the retention threshold.
#
# Usage:
#     fish run-retention.fish

set script_dir (dirname (realpath (status -f)))
set venv_python "$script_dir/.venv/bin/python"
set api_url "http://localhost:8002/api/retention/run"
set timeout 1800

if not test -x $venv_python
    echo "ERROR: Virtualenv python not found at $venv_python" >&2
    exit 1
end

echo "Triggering retention cleanup via HTTP API (timeout: "(
    math $timeout / 60
)" min)..."
echo ""

set response (curl -s -w "\n%{http_code}" -X POST $api_url --max-time $timeout)
set http_code (echo "$response" | tail -1)
set body (echo "$response" | head -n -1)

switch $http_code
    case 200
        echo "Retention completed successfully:"
        echo "$body" | $venv_python -m json.tool
    case 502 503 504
        echo "ERROR: Service unavailable (HTTP $http_code) — the app may be overloaded or not running." >&2
        exit 1
    case 000
        echo "ERROR: Retention timed out after "(math $timeout / 60)" min." >&2
        echo "Check logs for partial progress:" >&2
        echo "  sudo fish manage-service.fish logs | grep retention" >&2
        exit 1
    case "*"
        echo "ERROR: Retention failed (HTTP $http_code)" >&2
        echo "$body" | $venv_python -m json.tool 2>/dev/null
        exit 1
end
