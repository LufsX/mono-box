#!/sbin/sh

module_dir="/data/adb/modules/mono_box"

[ -n "$(magisk -v | grep lite)" ] && module_dir=/data/adb/lite_modules/mono_box

controller_path="/data/adb/box/scripts/box.controller"

(
    until [ $(getprop sys.boot_completed) -eq 1 ]; do
        sleep 3
    done
    ${controller_path} boot
) &

inotifyd ${controller_path} ${module_dir} > /dev/null 2>&1 &

while [ ! -f /data/misc/net/rt_tables ]; do
    sleep 3
done

inotifyd ${controller_path} /data/misc/net/rt_tables > /dev/null 2>&1 &
