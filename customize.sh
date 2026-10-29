#!/system/bin/sh

SKIPUNZIP=1
ASH_STANDALONE=1

choose_with_volume() {
    local prompt="$1" up_label="$2" down_label="$3"
    local timeout_sec="${4:-10}" choice result
    ui_print "- ${prompt} (${timeout_sec}s timeout, auto-skip)"
    ui_print "    Vol+ : ${up_label}"
    ui_print "    Vol- : ${down_label}"

    choice=$("$MODPATH/bin/monobox" --module-dir "$MODPATH" choose-volume "$timeout_sec")
    result=$?
    case "$result:$choice" in
        0:up) return 0 ;;
        0:down) return 1 ;;
        1:*) ui_print "    No input, skipping..." ;;
        *) ui_print "    Volume keys unavailable, skipping..." ;;
    esac
    return 1
}

maybe_download_core_with_volume() {
    local kind=mihomo overwrite=false proxy=false
    if [ -s /data/adb/box/bin/mihomo ]; then
        choose_with_volume "Core exists, overwrite it?" Yes No || return 0
        overwrite=true
    else
        choose_with_volume "Download core now?" Yes No || return 0
    fi
    if choose_with_volume "Use GitHub acceleration?" Yes No; then
        proxy=true
    fi
    if choose_with_volume "Choose core type" mihomo mihomo_smart; then
        kind=mihomo
    else
        kind=mihomo_smart
    fi
    if ! "$MODPATH/bin/monobox" --module-dir "$MODPATH" download-core "$kind" "$overwrite" "$proxy" "$device_abi"; then
        ui_print "- Core download failed; place mihomo in /data/adb/box/bin/ to retry manually."
    fi
}

[ "$BOOTMODE" = true ] || abort "Error: Install from Magisk, KernelSU or APatch Manager"
if [ "$KSU" = true ] && [ "$KSU_VER_CODE" -lt 10670 ]; then
    abort "Error: Please update KernelSU"
fi
unzip -qo "$ZIPFILE" -x 'META-INF/*' -d "$MODPATH" || abort "Error: Module extraction failed"
device_abi=$(getprop ro.product.cpu.abi)
case "$device_abi" in
    arm64-v8a|aarch64) device_abi=arm64-v8a ;;
    armeabi-v7a|armv7*|arm) device_abi=armeabi-v7a ;;
    x86_64|amd64) device_abi=x86_64 ;;
    x86|i686) device_abi=x86 ;;
    *) abort "Error: Unsupported ABI: $device_abi" ;;
esac
native_source="$MODPATH/bin/monobox-$device_abi"
[ -f "$native_source" ] || abort "Error: Native controller missing for $device_abi"

mkdir -p /data/adb/box/bin /data/adb/box/run /data/adb/box/scripts /data/adb/box/mihomo ||
    abort "Error: Cannot create runtime directories"

if [ -x /data/adb/box/bin/monobox ]; then
    /data/adb/box/bin/monobox watch-stop || abort "Error: Cannot stop the existing controller"
fi
for pid in $(pidof inotifyd 2>/dev/null); do
    if tr '\000' '\n' < "/proc/$pid/cmdline" 2>/dev/null | grep -qx /data/adb/box/scripts/box.controller; then
        kill "$pid" 2>/dev/null
    fi
done

mv -f "$native_source" "$MODPATH/bin/monobox" &&
    chmod 0700 "$MODPATH/bin/monobox" ||
    abort "Error: Native controller installation failed"
rm -f /data/adb/box/bin/monobox /data/adb/box/bin/.monobox.new

for script in box.service box.controller; do
    cp -f "$MODPATH/box/scripts/$script" "/data/adb/box/scripts/.$script.new" &&
        chmod 0700 "/data/adb/box/scripts/.$script.new" &&
        mv -f "/data/adb/box/scripts/.$script.new" "/data/adb/box/scripts/$script" ||
        abort "Error: Cannot install $script"
done
if [ ! -f /data/adb/box/scripts/box.config ]; then
    cp "$MODPATH/box/scripts/box.config" /data/adb/box/scripts/box.config ||
        abort "Error: Cannot install box.config"
    chmod 0600 /data/adb/box/scripts/box.config
fi
if [ ! -f /data/adb/box/mihomo/config.yaml ]; then
    cp "$MODPATH/box/mihomo/config.yaml" /data/adb/box/mihomo/config.yaml ||
        abort "Error: Cannot install default Mihomo config"
fi
rm -f /data/adb/box/scripts/box.env /data/adb/box/scripts/box.network \
    /data/adb/box/scripts/box.policy /data/adb/box/scripts/box.clash \
    /data/adb/box/scripts/box.core /data/adb/box/scripts/box.utils
rm -rf "$MODPATH/box"

maybe_download_core_with_volume

# Managers run the active module's service.sh after module updates are applied.
rm -f /data/adb/service.d/box_service.sh /data/adb/ksu/service.d/box_service.sh
set_perm_recursive "$MODPATH" 0 0 0755 0644
set_perm "$MODPATH/action.sh" 0 0 0700
set_perm "$MODPATH/service.sh" 0 0 0700
set_perm "$MODPATH/uninstall.sh" 0 0 0700
set_perm "$MODPATH/bin/monobox" 0 0 0700

ui_print "- Native controller installed in module ($device_abi); existing configuration retained"
rm -f "$MODPATH/customize.sh"
