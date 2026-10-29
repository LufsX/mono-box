#!/system/bin/sh

export PATH="/data/adb/magisk:/data/adb/ksu/bin:/data/adb/ap/bin:$PATH"
module_dir=${MODPATH:-$(CDPATH= cd "$(dirname "$0")" && pwd)}
native="$module_dir/bin/monobox"
if [ -x "$native" ]; then
    "$native" --module-dir "$module_dir" watch-stop
    "$native" --module-dir "$module_dir" service stop
fi

for pid in $(pidof inotifyd 2>/dev/null); do
    if tr '\000' '\n' < "/proc/$pid/cmdline" 2>/dev/null | grep -qx /data/adb/box/scripts/box.controller; then
        kill "$pid" 2>/dev/null
    fi
done
rm -f /data/adb/service.d/box_service.sh /data/adb/ksu/service.d/box_service.sh
rm -f /data/adb/box/bin/monobox /data/adb/box/bin/.monobox.new
