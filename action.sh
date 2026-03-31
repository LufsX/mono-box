#!/system/bin/sh

service_path="/data/adb/box/scripts/box.service"
utils_path="/data/adb/box/scripts/box.utils"
config_path="/data/adb/box/scripts/box.config"

usage() {
    echo "Usage: $0 [toggle|start|stop|restart|status|switch_mode <mode>|switch_tun <true/false>|get_configs|upgrade_core]"
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

switch_mode() {
    local mode=$1
    if [ -f "$config_path" ] && [ -f "$utils_path" ]; then
        . "$config_path"
        . "$utils_path"
        setMode "$mode"
        echo "Mode switched to $mode"
    else
        echo "Scripts not found"
        exit 1
    fi
}

switch_tun() {
    local enable=$1
    if [ -f "$config_path" ] && [ -f "$utils_path" ]; then
        . "$config_path"
        . "$utils_path"
        setTun "$enable"
        echo "TUN mode switched to $enable"
    else
        echo "Scripts not found"
        exit 1
    fi
}

get_configs() {
    if [ -f "$config_path" ] && [ -f "$utils_path" ]; then
        . "$config_path"
        . "$utils_path"
        getConfigs
    else
        echo "Scripts not found"
        exit 1
    fi
}

upgrade_core() {
    if [ -f "$config_path" ] && [ -f "$utils_path" ]; then
        . "$config_path"
        . "$utils_path"
        echo "Upgrading core..."
        upgradeResult=$(upgradeCore)
        echo "$upgradeResult"
    else
        echo "Scripts not found"
        exit 1
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
    switch_mode)
        switch_mode "$2"
        ;;
    switch_tun)
        switch_tun "$2"
        ;;
    get_configs)
        get_configs
        ;;
    upgrade_core)
        upgrade_core
        ;;
    -h | --help)
        usage
        ;;
    *)
        # 允许在WEB UI自定义传入其他参数执行，可作为预留扩展
        ensure_service
        "${service_path}" "$@"
        ;;
esac
