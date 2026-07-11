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

if not test -x $venv_python
    echo "ERROR: Virtualenv python not found at $venv_python" >&2
    exit 1
end

echo "Triggering retention cleanup via HTTP API..."
curl -s -X POST http://localhost:8002/api/retention/run --max-time 300 | $venv_python -m json.tool
