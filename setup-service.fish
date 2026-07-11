#!/usr/bin/env fish

# Setup the Camera Monitor systemd service.
#
# Legacy wrapper that delegates to manage-service.fish.
# Kept for backward compatibility.
#
# Usage:
#     fish setup-service.fish              # Install and start
#     fish setup-service.fish --status     # Show service status after install

set script_dir (dirname (realpath (status -f)))
set service_file "$script_dir/camera-monitor.service"

# Validate service file exists
if not test -f "$service_file"
    echo "ERROR: camera-monitor.service not found at $service_file" >&2
    exit 1
end

# Check for root
if not test (id -u) -eq 0
    echo "ERROR: This script must be run as root (sudo fish setup-service.fish)" >&2
    exit 1
end

set do_status false
for arg in $argv
    if test "$arg" = "--status"
        set do_status true
    end
end

# Delegate to manage-service.fish
fish $script_dir/manage-service.fish install

# Show status if requested
if test "$do_status" = true
    echo ""
    fish $script_dir/manage-service.fish status
end
