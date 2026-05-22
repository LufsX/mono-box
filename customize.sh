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
    local event pid waiter

    ui_print "- ${prompt} (${timeout_sec}s timeout, auto-skip)"
    ui_print "    Vol+ : ${up_label}"
    ui_print "    Vol- : ${down_label}"

    event=$(
        /system/bin/getevent -qlc 1 &
        pid=$!
        (sleep "${timeout_sec}"; kill "${pid}" 2>/dev/null) &
        waiter=$!
        wait "${pid}" 2>/dev/null
        kill "${waiter}" 2>/dev/null
        wait "${waiter}" 2>/dev/null
    )

    if [ -z "${event}" ]; then
        ui_print "    No input, skipping..."
        return 1
    fi

    if echo "${event}" | grep -q "KEY_VOLUMEUP.*DOWN"; then
        wait_key_release "KEY_VOLUMEUP"
        sleep 0.15
        return 0
    fi

    if echo "${event}" | grep -q "KEY_VOLUMEDOWN.*DOWN"; then
        wait_key_release "KEY_VOLUMEDOWN"
        sleep 0.15
        return 1
    fi

    return 1
}

wrap_github_url() {
    local url="$1"

    if [ "${USE_GITHUB_PROXY}" = "true" ]; then
        printf "https://cors.isteed.cc/%s" "${url}"
        return
    fi

    printf "%s" "${url}"
}

download_file_with_progress() {
    local source_url="$1"
    local output_file="$2"
    local header
    local total_bytes=""
    local next_percent=10
    local elapsed=0
    local report_interval=4

    rm -f "${output_file}" > /dev/null 2>&1

    header=$(curl -fsSLI --connect-timeout 15 --retry 1 "${source_url}" 2> /dev/null) || true
    total_bytes=$(printf "%s\n" "${header}" | tr -d '\r' | awk 'BEGIN{IGNORECASE=1} /^Content-Length:/ {print $2; exit}')

    curl -fL --connect-timeout 20 --retry 3 --retry-delay 2 --retry-all-errors -o "${output_file}" "${source_url}" > /dev/null 2>&1 &
    local curl_pid=$!

    while kill -0 "${curl_pid}" > /dev/null 2>&1; do
        sleep 1
        elapsed=$((elapsed + 1))

        local current_bytes
        current_bytes=$(wc -c < "${output_file}" 2> /dev/null)
        [ -n "${current_bytes}" ] || current_bytes=0

        if [ -n "${total_bytes}" ] && [ "${total_bytes}" -gt 0 ] 2> /dev/null; then
            local percent
            percent=$((current_bytes * 100 / total_bytes))
            [ "${percent}" -gt 99 ] && percent=99

            while [ "${percent}" -ge "${next_percent}" ]; do
                local current_mb total_mb
                current_mb=$(awk "BEGIN {printf \"%.1f\", ${current_bytes}/1024/1024}")
                total_mb=$(awk "BEGIN {printf \"%.1f\", ${total_bytes}/1024/1024}")
                ui_print "    Progress: ${next_percent}% (${current_mb}MB/${total_mb}MB)"
                next_percent=$((next_percent + 10))
            done
        elif [ $((elapsed % report_interval)) -eq 0 ]; then
            local current_mb
            current_mb=$(awk "BEGIN {printf \"%.1f\", ${current_bytes}/1024/1024}")
            ui_print "    Progress: downloading... (${current_mb}MB)"
        fi
    done

    wait "${curl_pid}"
    local curl_rc=$?

    if [ "${curl_rc}" -ne 0 ]; then
        return 1
    fi

    if [ -n "${total_bytes}" ] && [ "${total_bytes}" -gt 0 ] 2> /dev/null; then
        local current_mb total_mb
        current_mb=$(awk "BEGIN {printf \"%.1f\", ${total_bytes}/1024/1024}")
        total_mb=$(awk "BEGIN {printf \"%.1f\", ${total_bytes}/1024/1024}")
        ui_print "    Progress: 100% (${current_mb}MB/${total_mb}MB)"
    else
        local current_bytes current_mb
        current_bytes=$(wc -c < "${output_file}" 2> /dev/null)
        [ -n "${current_bytes}" ] || current_bytes=0
        current_mb=$(awk "BEGIN {printf \"%.1f\", ${current_bytes}/1024/1024}")
        ui_print "    Progress: completed (${current_mb}MB)"
    fi

    return 0
}

download_asset() {
    local origin_url="$1"
    local output_file="$2"
    local primary_url

    primary_url="$(wrap_github_url "${origin_url}")"
    download_file_with_progress "${primary_url}" "${output_file}"
}

core_target_path() {
    printf "%s" "${BOX_CORE_TARGET:-/data/adb/box/bin/mihomo}"
}

detect_arch() {
    DEVICE_ABI="$(getprop ro.product.cpu.abi)"
    ARCH_NAME="${DEVICE_ABI}"

    case "${DEVICE_ABI}" in
        arm64-v8a | aarch64)
            ARCH_NAME="arm64-v8a"
            ;;
        armeabi-v7a | armv7* | arm)
            ARCH_NAME="armeabi-v7a"
            ;;
        x86_64 | amd64)
            ARCH_NAME="x86_64"
            ;;
        x86)
            ARCH_NAME="x86"
            ;;
    esac
}

filter_urls_by_arch() {
    case "${ARCH_NAME}" in
        arm64-v8a)
            printf "%s\n" "$1" | grep -Ei 'arm64|aarch64|arm64-v8a'
            ;;
        armeabi-v7a)
            printf "%s\n" "$1" | grep -Ei 'armeabi-v7a|armv7|armv7a'
            ;;
        x86_64)
            printf "%s\n" "$1" | grep -Ei 'x86_64|amd64'
            ;;
        x86)
            printf "%s\n" "$1" | grep -Ei 'x86' | grep -Eiv 'x86_64|amd64'
            ;;
        *)
            printf "%s\n" "$1"
            ;;
    esac
}

extract_urls_from_release_api() {
    local api_url="$1"
    local response

    response=$(curl -fsSL --connect-timeout 20 --retry 2 "$(wrap_github_url "${api_url}")") || return 1

    printf "%s\n" "${response}" \
        | grep -o '"browser_download_url"[[:space:]]*:[[:space:]]*"[^"]*"' \
        | sed -E 's/.*"([^"]*)"/\1/'
}

extract_urls_from_expanded_assets() {
    local page_url="$1"
    local response

    response=$(curl -fsSL --connect-timeout 20 --retry 2 "$(wrap_github_url "${page_url}")") || return 1

    printf "%s\n" "${response}" \
        | grep -oE 'href="[^"]+/releases/download/Prerelease-Alpha/[^"]+"' \
        | sed -E 's/^href="//; s/"$//' \
        | sed 's#^/#https://github.com/#' \
        | sed 's/&amp;/\&/g' \
        | awk '!seen[$0]++'
}

pick_download_url() {
    local urls="$1"
    local prefer_regex="$2"
    local selected
    local filtered

    filtered=$(filter_urls_by_arch "${urls}" | grep -Ei 'android' | grep -Eiv '\.(sha256|sha256sum|sig|txt|json)$')
    if [ -z "${filtered}" ]; then
        filtered=$(filter_urls_by_arch "${urls}" | grep -Eiv '\.(sha256|sha256sum|sig|txt|json)$')
    fi

    if [ -n "${prefer_regex}" ]; then
        selected=$(printf "%s\n" "${filtered}" | grep -Ei "${prefer_regex}" | head -n 1)
    fi

    if [ -z "${selected}" ]; then
        selected=$(printf "%s\n" "${filtered}" | grep -Ei '\.tar\.gz$|\.tgz$|\.gz$|\.zip$' | head -n 1)
    fi

    if [ -z "${selected}" ]; then
        selected=$(printf "%s\n" "${filtered}" | head -n 1)
    fi

    printf "%s" "${selected}"
}

install_downloaded_core() {
    local asset_file="$1"
    local asset_name="$2"
    local workdir="$3"
    local target
    local extracted

    target="$(core_target_path)"
    mkdir -p "$(dirname "${target}")" > /dev/null 2>&1 || return 1

    case "${asset_name}" in
        *.tar.gz | *.tgz)
            tar -xzf "${asset_file}" -C "${workdir}" > /dev/null 2>&1 || return 1
            extracted=$(find "${workdir}" -type f -name 'mihomo*' | grep -Eiv '\.(txt|md|json)$' | head -n 1)
            [ -n "${extracted}" ] || return 1
            cat "${extracted}" > "${target}"
            ;;
        *.gz)
            gzip -dc "${asset_file}" > "${target}" 2> /dev/null || gunzip -c "${asset_file}" > "${target}" 2> /dev/null || return 1
            ;;
        *.zip)
            if command -v unzip > /dev/null 2>&1; then
                unzip -qo "${asset_file}" -d "${workdir}" > /dev/null 2>&1 || return 1
            elif command -v busybox > /dev/null 2>&1; then
                busybox unzip -qo "${asset_file}" -d "${workdir}" > /dev/null 2>&1 || return 1
            else
                return 1
            fi
            extracted=$(find "${workdir}" -type f -name 'mihomo*' | grep -Eiv '\.(txt|md|json)$' | head -n 1)
            [ -n "${extracted}" ] || return 1
            cat "${extracted}" > "${target}"
            ;;
        *)
            cat "${asset_file}" > "${target}"
            ;;
    esac

    chmod 0700 "${target}" > /dev/null 2>&1 || true
    return 0
}

download_core_by_choice() {
    local core_type="$1"
    local allow_overwrite="${2:-false}"
    local urls=""
    local download_url=""
    local temp_base="${BOX_CORE_TMPDIR:-/data/local/tmp}"
    local temp_dir="${temp_base}/mono_box_core_$$"
    local target
    local asset_name=""
    local download_file=""

    ui_print "- Core download plan"
    ui_print "    Selected core type : ${core_type}"
    ui_print "  - [1/4] Preparing download environment"

    if ! command -v curl > /dev/null 2>&1; then
        ui_print "    Error: curl is missing, skip core download."
        return 1
    fi

    target="$(core_target_path)"
    if [ -s "${target}" ] && [ "${allow_overwrite}" != "true" ]; then
        ui_print "    Core already exists, skip download: ${target}"
        return 0
    fi

    if [ -s "${target}" ] && [ "${allow_overwrite}" = "true" ]; then
        ui_print "    Core exists and will be overwritten: ${target}"
    fi

    detect_arch
    ui_print "    Device ABI : ${ARCH_NAME}"
    ui_print "  - [2/4] Fetching release asset list"

    if [ "${core_type}" = "mihomo" ]; then
        urls=$(extract_urls_from_release_api "https://api.github.com/repos/MetaCubeX/mihomo/releases") || return 1
        download_url=$(pick_download_url "${urls}" "mihomo")
    else
        urls=$(extract_urls_from_expanded_assets "https://github.com/vernesong/mihomo/releases/expanded_assets/Prerelease-Alpha") || return 1
        download_url=$(pick_download_url "${urls}" 'smart|compatible|alpha|mihomo')
    fi

    if [ -z "${download_url}" ]; then
        ui_print "    Error: no matching ${core_type} asset found for ${ARCH_NAME}."
        return 1
    fi

    mkdir -p "${temp_dir}"
    asset_name="${download_url##*/}"
    asset_name="${asset_name%%\?*}"
    download_file="${temp_dir}/${asset_name}"

    ui_print "  - [3/4] Downloading core asset"
    ui_print "    Asset: ${asset_name}"
    download_asset "${download_url}" "${download_file}" || {
        rm -rf "${temp_dir}" > /dev/null 2>&1
        return 1
    }

    ui_print "  - [4/4] Installing core binary"
    install_downloaded_core "${download_file}" "${asset_name}" "${temp_dir}" || {
        rm -rf "${temp_dir}" > /dev/null 2>&1
        return 1
    }

    rm -rf "${temp_dir}" > /dev/null 2>&1
    ui_print "    Installed ${core_type} to ${target}"
    return 0
}

maybe_download_core_with_volume() {
    local core_type="mihomo"
    local allow_overwrite="false"
    local target

    target="$(core_target_path)"
    if [ -s "${target}" ]; then
        ui_print "- Core status"
        ui_print "    Existing core: ${target}"

        if choose_with_volume "Core exists, overwrite it?" "Yes" "No"; then
            allow_overwrite="true"
            ui_print "    Selected: overwrite enabled"
        else
            ui_print "    Selected: keep existing core, skip download"
            return 0
        fi
    else
        if choose_with_volume "Download core now?" "Yes" "No"; then
            ui_print "    Selected: start core download"
        else
            ui_print "    Selected: skip core download"
            return 0
        fi
    fi

    if choose_with_volume "Use GitHub acceleration?" "Yes" "No"; then
        USE_GITHUB_PROXY="true"
        ui_print "    Selected: GitHub acceleration enabled"
    else
        USE_GITHUB_PROXY="false"
        ui_print "    Selected: GitHub acceleration disabled"
    fi

    if choose_with_volume "Choose core type" "mihomo" "mihomo_smart"; then
        core_type="mihomo"
        ui_print "    Selected core type: mihomo"
    else
        core_type="mihomo_smart"
        ui_print "    Selected core type: mihomo_smart"
    fi

    if download_core_by_choice "${core_type}" "${allow_overwrite}"; then
        ui_print "- Core download finished"
    else
        ui_print "- Core download failed"
        ui_print "    Please download core manually later."
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
