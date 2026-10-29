#!/system/bin/sh

export PATH="/data/adb/magisk:/data/adb/ksu/bin:/data/adb/ap/bin:$PATH"
module_dir=$(CDPATH= cd "$(dirname "$0")" && pwd) || exit 1
exec "$module_dir/bin/monobox" --module-dir "$module_dir" action "$@"
