#!/usr/bin/env sh
set -eu

cd "$(dirname "$0")"
PROJECT_DIR=$(pwd)
usage() {
    echo "Usage: sh build.sh [--prerelease] [--arch ARCH[,ARCH...]] [--webui-dir DIR]"
    echo "--webui-dir DIR reuses a precompiled WebUI without pnpm; relative paths use the project root."
    echo "Architectures: arm64 (default), arm, amd64, 386, all"
    echo "Android ABI names arm64-v8a, armeabi-v7a, x86_64 and x86 are also accepted."
    echo "Each ABI produces its own ZIP and update manifest."
    echo "MONOBOX_VERSION_CODE overrides the Asia/Shanghai timestamp (positive JS-safe integer)."
}

PRERELEASE=0
WEBUI_DIR=
ARCH_LIST=
while [ "$#" -gt 0 ]; do
    case "$1" in
        --prerelease) PRERELEASE=1 ;;
        --arch)
            [ "$#" -ge 2 ] && [ -n "$2" ] || { echo "--arch requires a value" >&2; exit 1; }
            ARCH_LIST="${ARCH_LIST:+$ARCH_LIST,}$2"
            shift
            ;;
        --arch=*)
            value=${1#--arch=}
            [ -n "$value" ] || { echo "--arch requires a value" >&2; exit 1; }
            ARCH_LIST="${ARCH_LIST:+$ARCH_LIST,}$value"
            ;;
        --webui-dir)
            [ "$#" -ge 2 ] && [ -n "$2" ] || { echo "--webui-dir requires a value" >&2; exit 1; }
            case "$2" in --*) echo "--webui-dir requires a value" >&2; exit 1 ;; esac
            WEBUI_DIR=$2
            shift
            ;;
        --webui-dir=*)
            WEBUI_DIR=${1#--webui-dir=}
            [ -n "$WEBUI_DIR" ] || { echo "--webui-dir requires a value" >&2; exit 1; }
            ;;
        -h|--help) usage; exit 0 ;;
        *) echo "Unknown argument: $1" >&2; usage >&2; exit 1 ;;
    esac
    shift
done

ARCH_LIST=${ARCH_LIST:-arm64}
case "$ARCH_LIST" in
    ,*|*,|*,,*) echo "Empty architecture in --arch" >&2; exit 1 ;;
esac
TARGETS=
set -f
for name in $(printf '%s' "$ARCH_LIST" | tr ',' ' '); do
    case "$name" in
        arm64|arm64-v8a|aarch64) selected=arm64-v8a:arm64 ;;
        arm|armv7|armeabi-v7a) selected=armeabi-v7a:arm ;;
        amd64|x86_64) selected=x86_64:amd64 ;;
        386|x86) selected=x86:386 ;;
        all) selected="arm64-v8a:arm64 armeabi-v7a:arm x86_64:amd64 x86:386" ;;
        *) echo "Unsupported architecture: $name" >&2; exit 1 ;;
    esac
    for target in $selected; do
        case " $TARGETS " in
            *" $target "*) ;;
            *) TARGETS="${TARGETS:+$TARGETS }$target" ;;
        esac
    done
done
[ -n "$TARGETS" ] || { echo "--arch requires at least one architecture" >&2; exit 1; }

command -v node >/dev/null 2>&1 || { echo "Node.js is required to generate update manifests" >&2; exit 1; }
if [ -n "$WEBUI_DIR" ]; then
    [ -d "$WEBUI_DIR" ] && [ -s "$WEBUI_DIR/index.html" ] && [ -d "$WEBUI_DIR/_app" ] || {
        echo "Invalid precompiled WebUI: $WEBUI_DIR" >&2
        exit 1
    }
else
    command -v pnpm >/dev/null 2>&1 || { echo "pnpm is required" >&2; exit 1; }
fi
command -v go >/dev/null 2>&1 || { echo "Go 1.24 or newer is required" >&2; exit 1; }
command -v upx >/dev/null 2>&1 || { echo "UPX is required to compress the native controller" >&2; exit 1; }

VERSION=$(awk -F= '/^version=/{print $2; exit}' module.prop)
[ -n "$VERSION" ] || { echo "Missing module version" >&2; exit 1; }
VERSION_CODE=${MONOBOX_VERSION_CODE-$(TZ=Asia/Shanghai date +%Y%m%d%H%M)}
VERSION_CODE=$(node - "$VERSION_CODE" <<'NODE'
const value = process.argv[2];
const code = Number(value);
if (value.length === 0 || /[^0-9]/.test(value) || !Number.isSafeInteger(code) || code <= 0) {
    console.error("MONOBOX_VERSION_CODE must contain only decimal digits and be a positive JavaScript safe integer (1..9007199254740991)");
    process.exit(1);
}
process.stdout.write(String(code));
NODE
)
RELEASES_URL="https://cors.isteed.cc/https://github.com/${GITHUB_REPOSITORY:-LufsX/mono-box}/releases"
RELEASE_TAG=$VERSION
UPDATE_BASE="$RELEASES_URL/latest/download"
CHANGELOG_URL="$UPDATE_BASE/changelog.md"
if [ "$PRERELEASE" -eq 1 ]; then
    RELEASE_TAG=Prerelease
    UPDATE_BASE="$RELEASES_URL/download/Prerelease"
    CHANGELOG_URL="$UPDATE_BASE/prerelease-changelog.md"
fi

STAGE=$(mktemp -d)
cleanup() { rm -rf "$STAGE"; }
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

if [ -n "$WEBUI_DIR" ]; then
    echo "[*] Reusing precompiled WebUI: $WEBUI_DIR"
else
    echo "[*] Building WebUI..."
    pnpm --dir webui install
    pnpm --dir webui run build
    WEBUI_DIR=webui/build
fi
cp -R "$WEBUI_DIR/." "$STAGE/webroot"
cp -R META-INF "$STAGE/META-INF"
cp action.sh service.sh customize.sh uninstall.sh README.md README_zh.md icon.png "$STAGE/"
mkdir -p "$STAGE/box/scripts" "$STAGE/bin" "$STAGE/box/mihomo"
cp box/scripts/box.config box/scripts/box.service box/scripts/box.controller "$STAGE/box/scripts/"
cp box/mihomo/config.yaml "$STAGE/box/mihomo/"

# Static Linux ELF uses Android's kernel ABI without a libc/NDK dependency.
# Runtime transport discovers Android DNS and loads Android system CA roots.
for target in $TARGETS; do
    abi=${target%:*}
    arch=${target#*:}
    rm -rf "$STAGE/bin"
    mkdir -p "$STAGE/bin"
    echo "[*] Building native controller for $abi..."
    CGO_ENABLED=0 GOOS=linux GOARCH="$arch" \
        go build -trimpath -ldflags='-s' \
        -o "$STAGE/bin/monobox-$abi" ./cmd/monobox
    echo "[*] Compressing native controller for $abi with UPX..."
    upx --lzma "$STAGE/bin/monobox-$abi"

    ZIP_NAME="MonoBox_${VERSION}_${abi}.zip"
    UPDATE_NAME="update-${abi}.json"
    awk '!/^versionCode=/ && !/^updateJson=/ && !/^moduleAbi=/' module.prop > "$STAGE/module.prop"
    printf 'versionCode=%s\nupdateJson=%s\nmoduleAbi=%s\n' "$VERSION_CODE" "$UPDATE_BASE/$UPDATE_NAME" "$abi" >> "$STAGE/module.prop"

    echo "[*] Creating module: $ZIP_NAME..."
    (
        cd "$STAGE"
        zip -qr -X "$STAGE/module.zip" .
    )
    mv -f "$STAGE/module.zip" "$PROJECT_DIR/$ZIP_NAME"
    node - "$VERSION" "$VERSION_CODE" "$RELEASES_URL/download/$RELEASE_TAG/$ZIP_NAME" "$CHANGELOG_URL" > "$PROJECT_DIR/$UPDATE_NAME" <<'NODE'
const [version, versionCode, zipUrl, changelog] = process.argv.slice(2);
process.stdout.write(JSON.stringify({
    version,
    versionCode: Number(versionCode),
    zipUrl,
    changelog,
}, null, 2) + "\n");
NODE
    if [ "$abi" = arm64-v8a ]; then
        cp "$PROJECT_DIR/$UPDATE_NAME" "$PROJECT_DIR/update.json"
    fi
    echo "[+] Done: $ZIP_NAME, $UPDATE_NAME"
done
