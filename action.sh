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
    local toggle_action="service"
    local toggle_tun_target="toggle"
    local toggle_mode_cycle="rule,global,direct"

    if [ -f "$config_path" ] && [ -f "$utils_path" ]; then
        . "$config_path"
        . "$utils_path"

        [ -n "$toggle_action" ] || toggle_action="service"
        [ -n "$toggle_tun_target" ] || toggle_tun_target="toggle"
        [ -n "$toggle_mode_cycle" ] || toggle_mode_cycle="rule,global,direct"
    fi

    if [ "$toggle_action" = "tun" ]; then
        local tun_target="$toggle_tun_target"

        if [ "$tun_target" = "toggle" ]; then
            local now_tun config_json
            config_json=$(getConfigs 2>/dev/null | tr -d '[:space:]')
            now_tun=$(printf '%s' "$config_json" | sed -n 's/.*"tun":{[^}]*"enable":\([^,}]*\).*/\1/p' | head -n 1)

            if [ "$now_tun" != "true" ] && [ "$now_tun" != "false" ]; then
                echo "Failed to read current TUN status from configs.tun.enable"
                exit 1
            fi

            if [ "$now_tun" = "true" ]; then
                tun_target="false"
            else
                tun_target="true"
            fi
        elif [ "$tun_target" = "on" ]; then
            tun_target="true"
        elif [ "$tun_target" = "off" ]; then
            tun_target="false"
        else
            echo "Invalid toggle_tun_target: $toggle_tun_target"
            exit 1
        fi

        setTun "$tun_target" > /dev/null 2>&1
        if [ $? -eq 0 ]; then
            echo "TUN switched to $tun_target"
            exit 0
        fi

        echo "Failed to switch TUN"
        exit 1
    fi

    if [ "$toggle_action" = "mode_cycle" ]; then
        local modes_raw modes current_mode first_mode next_mode found_current
        modes_raw=$(printf '%s' "$toggle_mode_cycle" | tr 'A-Z' 'a-z' | tr ';' ',' | tr -d '[:space:]')
        current_mode=$(getNowMode 2>/dev/null | tr 'A-Z' 'a-z')

        if [ -z "$modes_raw" ]; then
            modes_raw="rule,global,direct"
        fi

        modes=$(printf '%s' "$modes_raw" | tr ',' ' ')
        first_mode=""
        next_mode=""
        found_current="false"

        for mode in $modes; do
            case "$mode" in
                rule|global|direct)
                    [ -n "$first_mode" ] || first_mode="$mode"
                    if [ "$found_current" = "true" ] && [ -z "$next_mode" ]; then
                        next_mode="$mode"
                    fi
                    if [ "$mode" = "$current_mode" ]; then
                        found_current="true"
                    fi
                    ;;
            esac
        done

        if [ -z "$first_mode" ]; then
            first_mode="rule"
        fi

        [ -n "$next_mode" ] || next_mode="$first_mode"

        setMode "$next_mode" > /dev/null 2>&1
        if [ $? -eq 0 ]; then
            echo "Mode switched to $next_mode"
            exit 0
        fi

        echo "Failed to switch mode"
        exit 1
    fi

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
        usage
        exit 1
        ;;
esac
