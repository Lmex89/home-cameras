#!/usr/bin/env fish

# Generate the annotated timelapse video for a camera on a given date.
#
# Draws bounding boxes for the configured object classes (person, car,
# motorcycle by default) on each snapshot frame. Uses the same logic
# as the daily scheduled cron job but runs immediately on demand.
#
# The script calls POST /api/videos/annotated and waits for the server
# to generate the MP4. The resulting video is saved to data/videos/.
#
# Usage:
#     fish run-timelapse.fish [camera_id] [date] [classes] [host]
#
# Examples:
#     fish run-timelapse.fish                                              # camera 6, yesterday, default classes
#     fish run-timelapse.fish 6 2026-07-15                                  # specific date
#     fish run-timelapse.fish 6 2026-07-15 "person,car,truck"               # custom classes
#     fish run-timelapse.fish 6 2026-07-15 "" 192.168.1.100:8002           # remote host
#
# Behavior:
#     1. Resolves the project directory via realpath (works from any CWD).
#     2. Validates the virtualenv Python interpreter exists.
#     3. Resolves camera_id, date, and optional classes from arguments.
#     4. Sends a POST request to the running API server.
#     5. Reports elapsed time and the download URL for the generated video.
#
# Notes:
#     - The API server must be running (start with uvicorn or systemd).
#     - Default camera_id is 6; override with the first argument.
#     - Default date is yesterday; override with YYYY-MM-DD.
#     - Default classes are read from the server's TIMELAPSE_OBJECT_CLASSES config.
#     - Default host is localhost:8002; override with the 4th argument or
#       TIMELAPSE_HOST env var (e.g. ``192.168.1.100:8002``).
#     - Timeout is 10 minutes (600 seconds).

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
set camera_id $argv[1]
if test -z "$camera_id"
    set camera_id 5
    _log INFO "No camera_id provided, using default: $camera_id"
end

set target_date $argv[2]
if test -z "$target_date"
    read -p "echo \"[$(date '+%H:%M:%S')] $(set_color cyan)INPUT$(set_color normal) Date (YYYY-MM-DD, empty for yesterday): \"" target_date
    if test -z "$target_date"
        set target_date ($venv_python -c "from datetime import date, timedelta; print((date.today() - timedelta(days=1)).isoformat())")
        _log INFO "Using yesterday: $target_date"
    end
end

set classes $argv[3]

# Host: 4th argument, or TIMELAPSE_HOST env var, or default localhost:8002
set host $argv[4]
if test -z "$host"
    set host $TIMELAPSE_HOST
end
if test -z "$host"
    set host "localhost:8002"
end
set api_url "http://$host/api/videos/annotated"
set timeout 600

_log INFO "Starting annotated timelapse generation"
_log INFO "  Camera ID : $camera_id"
_log INFO "  Date      : $target_date"
if test -n "$classes"
    _log INFO "  Classes   : $classes"
else
    _log INFO "  Classes   : (config default)"
end
echo ""

# Build JSON payload
set json_payload "{\"camera_id\": $camera_id, \"date\": \"$target_date\""
if test -n "$classes"
    set json_payload "$json_payload, \"classes\": \"$classes\""
end
set json_payload "$json_payload}"

set start_epoch (date +%s)
_log INFO "Sending request to $api_url (timeout: "(math $timeout / 60)" min)..."

set response (curl -s -w "\n%{http_code}" -X POST $api_url \
    -H "Content-Type: application/json" \
    -d "$json_payload" \
    --max-time $timeout)
set http_code $response[-1]
set body $response[1..-2]
set elapsed (math (date +%s) - $start_epoch)

switch $http_code
    case 200
        _log OK "Timelapse generated in {$elapsed}s"
        echo ""
        printf "%s\n" $body | $venv_python -m json.tool
        echo ""
        set video_url (printf "%s\n" $body | $venv_python -c "import sys,json; print(json.load(sys.stdin)['video_url'])")
        set filename (printf "%s\n" $body | $venv_python -c "import sys,json; print(json.load(sys.stdin)['video_url'].split('/')[-1])")
        _log OK "Download: curl -o \"$filename\" http://$host$video_url"
        _log INFO "File saved to: $script_dir/data/videos/$filename"

    case 400
        _log ERROR "Invalid request (HTTP 400)"
        printf "%s\n" $body >&2
        exit 1

    case 404
        _log ERROR "Camera $camera_id not found (HTTP 404)"
        exit 1

    case 500
        _log ERROR "Timelapse generation failed (HTTP 500) after {$elapsed}s"
        printf "%s\n" $body | $venv_python -m json.tool 2>/dev/null
        exit 1

    case 502 503 504
        _log ERROR "Service unavailable (HTTP $http_code) after {$elapsed}s"
        _log WARN "The app may not be running at http://$host"
        exit 1

    case 000
        _log ERROR "Timelapse generation timed out after "(math $timeout / 60)" min"
        _log WARN "Check server logs for progress: sudo fish manage-service.fish logs | grep timelapse"
        exit 1

    case "*"
        _log ERROR "Timelapse generation failed (HTTP $http_code) after {$elapsed}s"
        printf "%s\n" $body >&2
        exit 1
end
