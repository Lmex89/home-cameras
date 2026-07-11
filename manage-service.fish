#!/usr/bin/env fish

# Manage the Camera Monitor systemd service.
#
# Provides start, stop, restart, status, logs, and uninstall commands.
# Must be run with sudo or as root.
#
# Usage:
#     fish manage-service.fish install            # Install and start
#     fish manage-service.fish start              # Start the service
#     fish manage-service.fish stop               # Stop the service
#     fish manage-service.fish restart            # Restart the service
#     fish manage-service.fish status             # Show service status
#     fish manage-service.fish logs               # Follow live logs
#     fish manage-service.fish uninstall          # Stop and remove the service
#     fish manage-service.fish reinstall          # Uninstall then install

set script_dir (dirname (realpath (status -f)))
set service_file "$script_dir/camera-monitor.service"
set service_name "camera-monitor.service"

# Check for root
if not test (id -u) -eq 0
    echo "ERROR: This script must be run as root (sudo fish manage-service.fish <command>)" >&2
    exit 1
end

if test (count $argv) -eq 0
    echo "Usage: fish manage-service.fish <command>"
    echo ""
    echo "Commands:"
    echo "  install     Install and start the service"
    echo "  start       Start the service"
    echo "  stop        Stop the service"
    echo "  restart     Restart the service"
    echo "  status      Show service status"
    echo "  logs        Follow live logs (journalctl)"
    echo "  uninstall   Stop and remove the service"
    echo "  reinstall   Uninstall then install fresh"
    exit 1
end

set command $argv[1]

switch "$command"
    case "install"
        if not test -f "$service_file"
            echo "ERROR: camera-monitor.service not found at $service_file" >&2
            exit 1
        end

        echo "Installing Camera Monitor systemd service..."
        cp "$service_file" /etc/systemd/system/$service_name
        systemctl daemon-reload
        systemctl enable $service_name
        systemctl start $service_name
        echo ""
        echo "Service installed and running."
        echo "  Status: sudo fish manage-service.fish status"
        echo "  Logs:   sudo fish manage-service.fish logs"

    case "start"
        systemctl start $service_name
        echo "Started $service_name"

    case "stop"
        systemctl stop $service_name
        echo "Stopped $service_name"

    case "restart"
        echo "Restarting Camera Monitor service..."
        systemctl stop $service_name

        # Wait for port 8002 to be released (up to 10 seconds)
        for i in (seq 1 10)
            if not ss -tlnp 2>/dev/null | grep -q ":8002 "
                echo "  Port 8002 is free"
                break
            end
            echo "  Waiting for port 8002... ($i/10)"
            sleep 1
        end

        systemctl start $service_name
        echo "Restarted $service_name"

    case "status"
        systemctl status $service_name --no-pager

    case "logs"
        journalctl -u $service_name -f

    case "uninstall"
        echo "Uninstalling Camera Monitor service..."
        systemctl stop $service_name 2>/dev/null
        systemctl disable $service_name 2>/dev/null
        rm -f /etc/systemd/system/$service_name
        systemctl daemon-reload
        echo "Service removed."

    case "reinstall"
        echo "Reinstalling Camera Monitor service..."
        systemctl stop $service_name 2>/dev/null
        systemctl disable $service_name 2>/dev/null
        rm -f /etc/systemd/system/$service_name
        systemctl daemon-reload

        if not test -f "$service_file"
            echo "ERROR: camera-monitor.service not found at $service_file" >&2
            exit 1
        end

        cp "$service_file" /etc/systemd/system/$service_name
        systemctl daemon-reload
        systemctl enable $service_name
        systemctl start $service_name
        echo ""
        echo "Service reinstalled and running."

    case "*"
        echo "ERROR: Unknown command '$command'" >&2
        echo ""
        echo "Usage: fish manage-service.fish <command>"
        echo ""
        echo "Commands:"
        echo "  install     Install and start the service"
        echo "  start       Start the service"
        echo "  stop        Stop the service"
        echo "  restart     Restart the service"
        echo "  status      Show service status"
        echo "  logs        Follow live logs (journalctl)"
        echo "  uninstall   Stop and remove the service"
        echo "  reinstall   Uninstall then install fresh"
        exit 1
end
