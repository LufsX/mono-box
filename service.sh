#!/system/bin/sh

export PATH="/data/adb/magisk:/data/adb/ksu/bin:/data/adb/ap/bin:$PATH"
module_dir=$(CDPATH= cd "$(dirname "$0")" && pwd) || exit 1
nohup "$module_dir/bin/monobox" --module-dir "$module_dir" watch > /dev/null 2>&1 < /dev/null &
