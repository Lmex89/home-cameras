#!/usr/bin/env fish

# Purge snapshots, analyses, videos and archives older than N days.
#
# This is a DESTRUCTIVE operation: it permanently deletes database rows
# and raw files, unlike run-retention.fish which archives them first.
#
# Usage:
#     fish run-purge.fish [days] [host]
#
# Examples:
#     fish run-purge.fish                         # keep last 3 days
#     fish run-purge.fish 7                       # keep last 7 days
#     fish run-purge.fish 3 192.168.1.100:8002    # remote host
#
# Notes:
#     - The API server must be running.
#     - Default days is 3; override with the first argument.
#     - Default host is localhost:8002; override with the 2nd argument or
#       PURGE_HOST env var.
#     - Capture and analysis schedulers are paused automatically while purging.
#     - Timeout is 30 minutes (1800 seconds).

function _log -a level msg
    set color
    switch $level
        case INFO
            set color (set_color cyan)
        case OK
            set color (set_color green)
        case WARN
            set color (set_color yellow)
        case ERROR
            set color (set_color red)
    end
    echo "[$(date '+%H:%M:%S')] $color$level$__fish_color_reset $msg"
end

set script_dir (dirname (realpath (status -f)))
set venv_python "$script_dir/.venv/bin/python"

if not test -x $venv_python
    _log ERROR "Virtualenv python not found at $venv_python"
    exit 1
end

# Parse arguments
set days $argv[1]
if test -z "$days"
    set days 3
    _log INFO "No days provided, using default: $days"
else
    if not string match -qr '^[0-9]+$' $days
        _log ERROR "Days must be a positive integer: $days"
        exit 1
    end
end

# Host: 2nd argument, or PURGE_HOST env var, or default localhost:8002
set host $argv[2]
if test -z "$host"
    set host $PURGE_HOST
end
if test -z "$host"
    set host "localhost:8002"
end
set api_url "http://$host/api/retention/purge"
set timeout 1800

_log WARN "This will PERMANENTLY DELETE data older than $days days"
_log WARN "Press Ctrl+C now to cancel, or wait 3 seconds..."
sleep 3

echo ""
_log INFO "Starting destructive purge"
_log INFO "  Keep days: $days"
_log INFO "  API URL  : $api_url"
echo ""

set start_epoch (date +%s)
_log INFO "Sending request..."

set response (curl -s -w "\n%{http_code}" -X POST $api_url \
    -H "Content-Type: application/json" \
    -d "{\"days\": $days}" \
    --max-time $timeout)
set http_code $response[-1]
set body $response[1..-2]
set elapsed (math (date +%s) - $start_epoch)

switch $http_code
    case 200
        _log OK "Purge completed in {$elapsed}s"
        echo ""
        printf "%s\n" $body | $venv_python -m json.tool
        echo ""

    case 422
        _log ERROR "Invalid request (HTTP 422)"
        printf "%s\n" $body | $venv_python -m json.tool 2>/dev/null
        exit 1

    case 500
        _log ERROR "Purge failed (HTTP 500) after {$elapsed}s"
        printf "%s\n" $body | $venv_python -m json.tool 2>/dev/null
        exit 1

    case 502 503 504
        _log ERROR "Service unavailable (HTTP $http_code) after {$elapsed}s"
        _log WARN "The app may not be running at http://$host"
        exit 1

    case 000
        _log ERROR "Purge timed out after "(math $timeout / 60)" min"
        _log WARN "Check server logs: sudo fish manage-service.fish logs | grep purge"
        exit 1

    case "*"
        _log ERROR "Purge failed (HTTP $http_code) after {$elapsed}s"
        printf "%s\n" $body >&2
        exit 1
end
