#!/usr/bin/env fish
# cameras.fish — single command to control the camera monitor service:
# build, launch in the background, stop, restart, inspect status and
# follow the logs. Resolves the project root from its own location, so it
# works from any directory.
#
# Usage:
#   cameras.fish start     check requirements, build and launch in background
#   cameras.fish stop      stop the running service
#   cameras.fish restart   stop, then start again
#   cameras.fish status    show state, PID, memory, uptime, health, detector
#   cameras.fish logs      follow the service console log (Ctrl-C to exit)
#   cameras.fish help      show this message

set -l SCRIPT_DIR (dirname (status --current-filename))
cd $SCRIPT_DIR

set -g BINARY bin/cameras-go
set -g PIDFILE data/cameras.pid
set -g LOGFILE data/logs/console.log
set -g PORT (grep -E '^PORT=' .env 2>/dev/null | head -1 | cut -d= -f2 | string trim)
if test -z "$PORT"
    set PORT 8004
end

# Prefer a locally-built OpenCV >= 4.8 (YOLOv8 needs it; the distro's
# 4.5.4 aborts) — see `make opencv-build`.
set -l OPENCV_PREFIX $HOME/.local/opencv
if test -d "$OPENCV_PREFIX/lib/pkgconfig"
    set -gx PKG_CONFIG_PATH "$OPENCV_PREFIX/lib/pkgconfig" $PKG_CONFIG_PATH
    set -gx LD_LIBRARY_PATH "$OPENCV_PREFIX/lib" $LD_LIBRARY_PATH
end

# Log one line with a timestamp, a severity tag and color.
#
# Args:
#   level: One of "INF", "WRN", "ERR", "OK"; selects the tag color.
#   message: The text to print to stdout.
#
# Returns:
#   Nothing.
function _log
    set -l level $argv[1]
    set -l message $argv[2]
    set -l ts (date '+%Y-%m-%d %H:%M:%S')
    switch $level
        case OK
            set_color green
        case WRN
            set_color yellow
        case ERR
            set_color red
        case '*'
            set_color normal
    end
    echo "[$ts] $level $message"
    set_color normal
end

# Print the usage / help text for every subcommand.
#
# Returns:
#   Nothing.
function _usage
    echo "Usage: cameras.fish <command>"
    echo
    echo "  start    check requirements, build (gocv engine when OpenCV >= 4.8 is present), launch in background"
    echo "  stop     stop the running service"
    echo "  restart  stop, then start again"
    echo "  status   show state, PID, memory, uptime, health and detector mode"
    echo "  logs     follow the service console log (Ctrl-C to exit)"
    echo "  help     this message"
end

# Read the PID of the running service from the PID file.
#
# Returns:
#   The PID as stdout, or nothing when no PID file exists.
function _pid
    test -f $PIDFILE; and cat $PIDFILE
end

# Check whether the service process is alive.
#
# Returns:
#   Exit 0 when the PID file exists and the process responds to kill -0.
function _is_running
    set -l pid (_pid)
    test -n "$pid"; and kill -0 $pid 2>/dev/null
end

# Build the server binary, preferring the gocv YOLO engine.
#
# Uses `make opencv` when OpenCV >= 4.8 is discoverable through
# pkg-config (YOLOv8 ONNX aborts on older versions), otherwise falls
# back to the stub-detector build and warns.
#
# Returns:
#   Exit 0 on success, nonzero when the build fails.
function _build
    if command -q pkg-config; and pkg-config --atleast-version=4.8 opencv4
        _log INF "OpenCV " (pkg-config --modversion opencv4) " detected — building with the gocv YOLO engine"
        make opencv
    else
        _log WRN "OpenCV >= 4.8 not found — building the stub detector (no real YOLO; run 'make opencv-build')"
        make build
    end
end

# Query the HTTP health endpoint and print the result.
#
# Returns:
#   Nothing. Prints the /healthz JSON body, or an explanation when the
#   endpoint is unreachable or curl is not installed.
function _status_health
    if command -q curl
        set -l body (curl -s -m 3 http://localhost:$PORT/healthz)
        if test -n "$body"
            echo "health:  $body"
        else
            echo "health:  unreachable on :$PORT"
        end
    else
        echo "health:  curl not installed — install it to check :$PORT"
    end
end

# Report whether the running service uses the gocv YOLO detector.
#
# Inspects the console log for the "object detector unavailable" startup
# warning; its absence means the gocv engine loaded the model at boot.
#
# Returns:
#   Nothing.
function _status_detector
    if not test -f $LOGFILE
        return 0
    end
    if grep -q "object detector unavailable" $LOGFILE
        echo "detector: STUB (no YOLO) — see log for the reason (missing model / no opencv tag)"
    else
        echo "detector: YOLO (gocv engine) — no stub warning at startup"
    end
end

# Start the service in the background after ensuring requirements.
#
# Runs `make requirements` (model download, ffmpeg/OpenCV checks), builds
# the binary via _build, then launches it with nohup and records the PID.
# A stale PID file from a previous run is removed first.
#
# Raises:
#   Exit 1: When the service is already running, or requirements/build fail.
#
# Returns:
#   Exit 0 on success.
function _start
    if _is_running
        _log WRN "already running (pid "(_pid)") — use 'restart'"
        return 1
    end
    rm -f $PIDFILE
    mkdir -p data/logs
    make requirements; or return 1
    _build; or return 1
    nohup $BINARY > $LOGFILE 2>&1 &
    echo $last_pid > $PIDFILE
    _log OK "started camera monitor (pid $last_pid)"
    _log INF "logs: $LOGFILE"
end

# Stop the running service, waiting up to 5 s before forcing the kill.
#
# Sends SIGTERM and polls the process every 250 ms; escalates to SIGKILL
# when it does not exit in time, then removes the PID file.
#
# Returns:
#   Exit 0 always (idempotent).
function _stop
    if not _is_running
        _log WRN "not running"
        return 0
    end
    set -l pid (_pid)
    kill $pid
    for i in (seq 1 20)
        if not kill -0 $pid 2>/dev/null
            rm -f $PIDFILE
            _log OK "stopped (pid $pid)"
            return 0
        end
        sleep 0.25
    end
    kill -9 $pid
    rm -f $PIDFILE
    _log WRN "stopped (pid $pid, force-killed)"
end

switch $argv[1]
    case start
        _start; or exit 1

    case stop
        _stop

    case restart
        _stop
        sleep 1
        _start; or exit 1

    case status
        if _is_running
            set -l pid (_pid)
            echo "state:   running (pid $pid)"
            set -l info (ps -o rss= -o etime= -p $pid 2>/dev/null)
            if test -n "$info"
                set -l rss (echo $info | awk '{print $1}')
                set -l etime (echo $info | awk '{print $2}')
                printf "memory:  %.1f MB\n" (math $rss / 1024)
                echo "uptime:  $etime"
            end
            _status_health
            _status_detector
        else
            echo "state:   stopped"
            echo "start:   ./cameras.fish start"
        end

    case logs
        if test -f $LOGFILE
            tail -f -n 50 $LOGFILE
        else
            _log WRN "no log yet — start the service first"
        end

    case help '-h' '--help' '*'
        _usage
end
