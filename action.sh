#!/system/bin/sh

service_path="/data/adb/box/scripts/box.service"

usage() {
    echo "Usage: $0 [toggle|start|stop|restart|status]"
}

ensure_service() {
    if [ ! -x "${service_path}" ]; then
        echo "Error: ${service_path} not found or not executable"
        exit 1
    fi
}

run_toggle() {
    if "${service_path}" status > /dev/null 2>&1; then
        echo "Toggling: stopping service..."
        "${service_path}" stop
        echo "Service stopped."
        sleep 1
    else
        echo "Toggling: starting service..."
        "${service_path}" start
        if [ $? -eq 0 ]; then
            echo "Service started."
        else
            echo "Failed to start service."
        fi
        sleep 1
    fi
}

case "$1" in
    "" | toggle)
        ensure_service
        run_toggle
        ;;
    start | stop | restart | status)
        ensure_service
        "${service_path}" "$1"
        ;;
    -h | --help)
        usage
        ;;
    *)
        usage
        exit 1
        ;;
esac
