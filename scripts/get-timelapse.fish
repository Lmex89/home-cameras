#!/usr/bin/env fish
# get-timelapse.fish requests annotated timelapses from the running
# camera service for a single date or an inclusive date range.
#
# Usage:
#   scripts/get-timelapse.fish --camera-id 6 --date 2026-08-17
#   scripts/get-timelapse.fish --camera-id 6 --from 2026-08-01 --to 2026-08-07
#   scripts/get-timelapse.fish --camera-id 6 --date 2026-08-17 --objects "person,car"
#   scripts/get-timelapse.fish --camera-id 6 --date 2026-08-17 --output-dir /tmp/timelapses
#
# Notes:
#   - By default, object classes come from the project's
#     TIMELAPSE_OBJECT_CLASSES setting on the backend.
#   - Videos are downloaded by default to data/videos/downloads.
#   - The backend must already be running.

set -l SCRIPT_DIR (dirname (status --current-filename))
set -l ROOT_DIR (realpath "$SCRIPT_DIR/..")
cd $ROOT_DIR

# _log print one timestamped log line.
#
# Args:
#   level: Severity tag (INF, WRN, ERR, OK).
#   message: Human-friendly message.
#
# Returns:
#   Nothing.
function _log
    set -l level $argv[1]
    set -l message (string join " " -- $argv[2..-1])
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

    echo "[$ts] $level $message" >&2
    set_color normal
end

# _die print an error and exit nonzero.
#
# Args:
#   message: Fatal error to print.
#
# Returns:
#   Nothing.
#
# Raises:
#   Exit 1: Always.
function _die
    _log ERR $argv
    exit 1
end

# _usage print usage help.
#
# Returns:
#   Nothing.
function _usage
    echo "Usage: scripts/get-timelapse.fish --camera-id <id> [--date YYYY-MM-DD | --from YYYY-MM-DD --to YYYY-MM-DD] [--objects class1,class2] [--base-url URL] [--output-dir PATH]"
    echo
    echo "Options:"
    echo "  -c, --camera-id   Camera id (required, integer >= 1)"
    echo "  -d, --date        Single date (YYYY-MM-DD)"
    echo "      --from        Range start date (YYYY-MM-DD)"
    echo "      --to          Range end date (YYYY-MM-DD, inclusive)"
    echo "  -o, --objects     Override classes, ex: person,car,motorcycle"
    echo "  -u, --base-url    API base URL (default: http://localhost:<PORT>)"
    echo "  -O, --output-dir  Download directory (default: data/videos/downloads)"
    echo "  -h, --help        Show this help"
end

# _valid_date validate one YYYY-MM-DD value.
#
# Args:
#   date_value: Candidate date.
#
# Returns:
#   Exit 0 when valid; nonzero otherwise.
function _valid_date
    set -l date_value $argv[1]
    if not string match -rq '^[0-9]{4}-[0-9]{2}-[0-9]{2}$' -- $date_value
        return 1
    end
    date -d "$date_value" '+%Y-%m-%d' >/dev/null 2>&1
end

# _resolve_port read the backend port from env/.env with fallback.
#
# Returns:
#   The resolved port as stdout.
function _resolve_port
    if set -q PORT; and test -n "$PORT"
        echo $PORT
        return 0
    end

    if test -f .env
        set -l line (grep -E '^PORT=' .env 2>/dev/null | head -1)
        if test -n "$line"
            set -l value (string split -m1 '=' -- $line)[2]
            set value (string trim -- $value)
            set value (string trim -c '"' -- $value)
            set value (string trim -c "'" -- $value)
            if string match -rq '^[0-9]+$' -- $value
                echo $value
                return 0
            end
        end
    end

    echo 8004
end

# _resolve_default_classes read default object classes from env/.env.
#
# Returns:
#   The resolved class list as stdout.
function _resolve_default_classes
    if set -q TIMELAPSE_OBJECT_CLASSES; and test -n "$TIMELAPSE_OBJECT_CLASSES"
        echo $TIMELAPSE_OBJECT_CLASSES
        return 0
    end

    if test -f .env
        set -l line (grep -E '^TIMELAPSE_OBJECT_CLASSES=' .env 2>/dev/null | head -1)
        if test -n "$line"
            set -l value (string split -m1 '=' -- $line)[2]
            set value (string trim -- $value)
            set value (string trim -c '"' -- $value)
            set value (string trim -c "'" -- $value)
            if test -n "$value"
                echo $value
                return 0
            end
        end
    end

    echo "person,car,motorcycle"
end

# _json_string extract a JSON string field with a lightweight regex.
#
# Args:
#   field: JSON key to read.
#   body: JSON payload as text.
#
# Returns:
#   The field value on stdout when present.
function _json_string
    set -l field $argv[1]
    set -l body $argv[2]
    set -l parts (string match -r '"'$field'"[[:space:]]*:[[:space:]]*"([^"]*)"' -- $body)
    if test (count $parts) -ge 2
        echo $parts[2]
    end
end

# _dates_between list every date in an inclusive range.
#
# Args:
#   start_date: Range start.
#   end_date: Range end.
#
# Returns:
#   One YYYY-MM-DD per line.
function _dates_between
    set -l start_date $argv[1]
    set -l end_date $argv[2]
    set -l current $start_date
    while true
        echo $current
        if test "$current" = "$end_date"
            break
        end
        set current (date -I -d "$current + 1 day")
    end
end

# _request_day request one annotated timelapse for a camera/day.
#
# Args:
#   camera_id: Camera id.
#   day: Date to render.
#   classes: Comma-separated classes.
#   base_url: API origin.
#   include_classes: "1" to include custom classes, "0" for backend defaults.
#
# Returns:
#   The returned video URL (absolute or relative) on success.
#
# Raises:
#   Exit 1: When the API request fails or response is invalid.
function _request_day
    set -l camera_id $argv[1]
    set -l day $argv[2]
    set -l classes $argv[3]
    set -l base_url $argv[4]
    set -l include_classes $argv[5]

    set -l payload ""
    if test "$include_classes" = "1"
        set payload '{"camera_id":'$camera_id',"date":"'$day'","classes":"'$classes'"}'
    else
        set payload '{"camera_id":'$camera_id',"date":"'$day'"}'
    end

    set -l body_file (mktemp)
    set -l status_code (curl -sS -X POST "$base_url/api/videos/annotated" -H "Content-Type: application/json" -d "$payload" -o $body_file -w "%{http_code}")
    set -l body (string collect < $body_file)
    rm -f $body_file

    if test "$status_code" = "000"
        _log ERR "camera $camera_id $day request failed: cannot reach $base_url"
        return 1
    end

    if test "$status_code" != "200"
        set -l detail (_json_string detail $body)
        set -l err_msg (_json_string error $body)
        if test -n "$detail"
            _log ERR "camera $camera_id $day rejected: $detail"
        else if test -n "$err_msg"
            _log ERR "camera $camera_id $day rejected: $err_msg"
        else
            _log ERR "camera $camera_id $day rejected: HTTP $status_code"
        end
        return 1
    end

    set -l video_url (_json_string video_url $body)
    if test -z "$video_url"
        _log ERR "camera $camera_id $day succeeded but response did not include video_url"
        return 1
    end

    if string match -rq '^https?://' -- $video_url
        echo $video_url
    else
        echo "$base_url$video_url"
    end
end

# _download_video download one video URL into the output directory.
#
# Args:
#   video_url: Source URL returned by the API.
#   output_dir: Target directory.
#   camera_id: Camera id (used for filename).
#   day: Date (used for filename).
#
# Returns:
#   The saved file path.
#
# Raises:
#   Exit 1: When the download fails.
function _download_video
    set -l video_url $argv[1]
    set -l output_dir $argv[2]
    set -l camera_id $argv[3]
    set -l day $argv[4]

    mkdir -p "$output_dir"; or begin
        _log ERR "cannot create output directory: $output_dir"
        return 1
    end

    set -l base_name "timelapse_annotated_"$camera_id"_"$day
    set -l candidate "$output_dir/$base_name.mp4"
    set -l suffix 1
    while test -e "$candidate"
        set candidate "$output_dir/$base_name"_"$suffix".mp4
        set suffix (math $suffix + 1)
    end
    if test $suffix -gt 1
        _log WRN "camera $camera_id $day existing file found; using new filename"
    end

    set -l tmp_path "$candidate.part"
    curl -fL -sS "$video_url" -o "$tmp_path"; or begin
        rm -f "$tmp_path"
        _log ERR "download failed: $video_url"
        return 1
    end

    mv "$tmp_path" "$candidate"; or begin
        rm -f "$tmp_path"
        _log ERR "cannot move downloaded file to $candidate"
        return 1
    end

    if command -q realpath
        realpath "$candidate"
    else
        echo "$candidate"
    end
end

if not command -q curl
    _die "curl is required"
end

set -l camera_id ""
set -l single_date ""
set -l from_date ""
set -l to_date ""
set -l objects ""
set -l base_url ""
set -l output_dir "data/videos/downloads"

while test (count $argv) -gt 0
    switch $argv[1]
        case -c --camera-id
            test (count $argv) -ge 2; or _die "--camera-id requires a value"
            set camera_id $argv[2]
            set -e argv[1..2]
        case -d --date
            test (count $argv) -ge 2; or _die "--date requires a value"
            set single_date $argv[2]
            set -e argv[1..2]
        case --from
            test (count $argv) -ge 2; or _die "--from requires a value"
            set from_date $argv[2]
            set -e argv[1..2]
        case --to
            test (count $argv) -ge 2; or _die "--to requires a value"
            set to_date $argv[2]
            set -e argv[1..2]
        case -o --objects
            test (count $argv) -ge 2; or _die "--objects requires a value"
            set objects $argv[2]
            set -e argv[1..2]
        case -u --base-url
            test (count $argv) -ge 2; or _die "--base-url requires a value"
            set base_url $argv[2]
            set -e argv[1..2]
        case -O --output-dir
            test (count $argv) -ge 2; or _die "--output-dir requires a value"
            set output_dir $argv[2]
            set -e argv[1..2]
        case -h --help
            _usage
            exit 0
        case '*'
            _die "unknown argument: $argv[1]"
    end
end

if test -z "$camera_id"
    _die "--camera-id is required"
end
if not string match -rq '^[0-9]+$' -- $camera_id
    _die "--camera-id must be an integer"
end
if test "$camera_id" -lt 1
    _die "--camera-id must be >= 1"
end

set -l using_single 0
if test -n "$single_date"
    set using_single 1
end

if test $using_single -eq 1
    if test -n "$from_date" -o -n "$to_date"
        _die "use either --date or --from/--to, not both"
    end
    _valid_date $single_date; or _die "--date must be YYYY-MM-DD"
else
    if test -z "$from_date" -o -z "$to_date"
        _die "use --date or provide both --from and --to"
    end
    _valid_date $from_date; or _die "--from must be YYYY-MM-DD"
    _valid_date $to_date; or _die "--to must be YYYY-MM-DD"

    set -l from_epoch (date -d "$from_date" +%s)
    set -l to_epoch (date -d "$to_date" +%s)
    if test $from_epoch -gt $to_epoch
        _die "--from must be earlier than or equal to --to"
    end
end

if test -z "$base_url"
    set -l port (_resolve_port)
    set base_url "http://localhost:$port"
end

set -l include_classes 0
set -l effective_classes (_resolve_default_classes)
if test -n "$objects"
    set effective_classes $objects
    set include_classes 1
end

if test $include_classes -eq 1
    _log INF "using custom object classes: $effective_classes"
else
    _log INF "using backend project default object classes: $effective_classes"
end
_log INF "request base URL: $base_url"
_log INF "download directory: $output_dir"

set -l dates
if test $using_single -eq 1
    set dates $single_date
else
    set dates (_dates_between $from_date $to_date)
end

set -l total_days (count $dates)
if test $using_single -eq 1
    _log INF "processing 1 day: $single_date"
else
    _log INF "processing $total_days days: $from_date to $to_date"
end

set -l failures 0
for day in $dates
    _log INF "generating annotated timelapse for camera $camera_id on $day"
    set -l video_url (_request_day $camera_id $day $effective_classes $base_url $include_classes)
    if test $status -ne 0
        set failures (math $failures + 1)
        continue
    end

    _log INF "downloading timelapse for camera $camera_id on $day"
    set -l saved_path (_download_video "$video_url" "$output_dir" "$camera_id" "$day")
    if test $status -ne 0
        set failures (math $failures + 1)
        continue
    end
    _log OK "$day -> $saved_path"
end

if test $failures -gt 0
    set -l success (math $total_days - $failures)
    _die "completed with errors: $success/$total_days succeeded, $failures failed"
end

_log OK "all timelapse requests completed: $total_days/$total_days succeeded"
