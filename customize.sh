#!/sbin/sh

SKIPUNZIP=1
ASH_STANDALONE=1

wait_key_release() {
    local key_name="$1"
    local event

    while true; do
        event=$(/system/bin/getevent -qlc 1 2> /dev/null)
        echo "${event}" | grep -q "${key_name}.*UP" && return 0
    done
}

choose_with_volume() {
    local prompt="$1"
    local up_label="$2"
    local down_label="$3"
    local timeout_sec="${4:-10}"
    local deadline event pid waiter

    ui_print "- ${prompt} (${timeout_sec}s timeout, auto-skip)"
    ui_print "    Vol+ : ${up_label}"
    ui_print "    Vol- : ${down_label}"

    deadline=$(($(date +%s) + timeout_sec))

    while [ "$(date +%s)" -lt "${deadline}" ] 2>/dev/null; do
        event=$(
            /system/bin/getevent -qlc 1 &
            pid=$!
            (sleep 2; kill "${pid}" 2>/dev/null) &
            waiter=$!
            wait "${pid}" 2>/dev/null
            kill "${waiter}" 2>/dev/null
            wait "${waiter}" 2>/dev/null
        )

        [ -z "${event}" ] && continue

        if printf "%s\n" "${event}" | grep -q "KEY_VOLUMEUP.*DOWN"; then
            wait_key_release "KEY_VOLUMEUP"
            sleep 0.15
            return 0
        fi

        if printf "%s\n" "${event}" | grep -q "KEY_VOLUMEDOWN.*DOWN"; then
            wait_key_release "KEY_VOLUMEDOWN"
            sleep 0.15
            return 1
        fi
    done

    ui_print "    No input, skipping..."
    return 1
}

maybe_download_core_with_volume() {
    local core_type="mihomo"
    local allow_overwrite="false"
    local target

    target="$(core_target_path)"
    if [ -s "${target}" ]; then
        if choose_with_volume "Core exists, overwrite it?" "Yes" "No"; then
            allow_overwrite="true"
        else
            return 0
        fi
    else
        if ! choose_with_volume "Download core now?" "Yes" "No"; then
            return 0
        fi
    fi

    if choose_with_volume "Use GitHub acceleration?" "Yes" "No"; then
        USE_GITHUB_PROXY="true"
    else
        USE_GITHUB_PROXY="false"
    fi

    if choose_with_volume "Choose core type" "mihomo" "mihomo_smart"; then
        core_type="mihomo"
    else
        core_type="mihomo_smart"
    fi

    if ! download_core_by_choice "${core_type}" "${allow_overwrite}"; then
        ui_print "- Core download failed, please download manually later."
    fi
}

if [ "$BOOTMODE" != true ]; then
    abort "Error: Please install in Magisk Manager, KernelSU Manager or APatch"
fi

if [ "$KSU" = true ] && [ "$KSU_VER_CODE" -lt 10670 ]; then
    abort "Error: Please update your KernelSU"
fi

if [ "$KSU" = true ] && [ "$KSU_VER_CODE" -lt 10683 ]; then
    service_dir="/data/adb/ksu/service.d"
else
    service_dir="/data/adb/service.d"
fi

if [ ! -d "$service_dir" ]; then
    mkdir -p "$service_dir"
fi

unzip -qo "${ZIPFILE}" -x 'META-INF/*' -d "$MODPATH"

if [ -d /data/adb/box ]; then
    if [ -f /data/adb/box/scripts/box.config ]; then
        cp /data/adb/box/scripts/box.config /data/adb/box/scripts/box.config.bak
        ui_print "- User configuration box.config has been backed up to box.config.bak"
    fi

    cp -f "$MODPATH"/box/scripts/* /data/adb/box/scripts/
    ui_print "- Module scripts have been refreshed"
    ui_print "    Please re-check box.config if needed."

    rm -rf "$MODPATH"/box
else
    mv "$MODPATH"/box /data/adb/
fi

mkdir -p /data/adb/box/bin/
mkdir -p /data/adb/box/run/

. /data/adb/box/scripts/box.core

maybe_download_core_with_volume

mv -f "$MODPATH"/box_service.sh "$service_dir"/

rm -f "$MODPATH"/customize.sh

set_perm_recursive "$MODPATH" 0 0 0755 0644
set_perm_recursive "$MODPATH"/action.sh 0 0 0755 0700
set_perm_recursive /data/adb/box/ 0 0 0755 0644
set_perm_recursive /data/adb/box/scripts/ 0 0 0755 0700
set_perm_recursive /data/adb/box/bin/ 0 0 0755 0700

set_perm "$service_dir"/box_service.sh 0 0 0700

# fix "set_perm_recursive /data/adb/box/scripts" not working on some phones.
chmod ugo+x /data/adb/box/scripts/*

for pid in $(pidof inotifyd); do
    if grep -qE 'box.controller' /proc/${pid}/cmdline; then
        kill ${pid}
    fi
done

inotifyd "/data/adb/box/scripts/box.controller" "$MODPATH" > /dev/null 2>&1 &

if [ -f /data/misc/net/rt_tables ]; then
    inotifyd "/data/adb/box/scripts/box.controller" /data/misc/net/rt_tables > /dev/null 2>&1 &
fi
