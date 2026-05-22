#!/usr/bin/env sh
set -e

PRERELEASE=0
for arg in "$@"; do
    if [ "$arg" = "--prerelease" ]; then
        PRERELEASE=1
    fi
done

echo "[*] Building WebUI..."
cd webui
if [ ! -d "node_modules" ]; then
    echo "[*] Installing dependencies with pnpm..."
    pnpm install
fi
pnpm run build
cd ..

echo "[*] Copying WebUI artifacts..."
rm -rf webroot
cp -r webui/build webroot

VERSION=$(grep '^version=' module.prop | awk -F '=' '{print $2}')
ZIP_NAME="MonoBox_${VERSION}.zip"
VERSION_CODE=$(TZ='Asia/Shanghai' date +%Y%m%d%H%M)
UPDATE_JSON="https://cors.isteed.cc/https://github.com/LufsX/mono-box/releases/latest/download/update.json"

if [ "$PRERELEASE" -eq 1 ]; then
    UPDATE_JSON="https://cors.isteed.cc/https://github.com/LufsX/mono-box/releases/download/Prerelease/update.json"
fi

echo "[*] Injecting dynamic versionCode and updateJson: ${VERSION_CODE}"
cp module.prop module.prop.bak
cp module.prop module.prop.tmp
echo "versionCode=${VERSION_CODE}" >> module.prop.tmp
echo "updateJson=${UPDATE_JSON}" >> module.prop.tmp
mv module.prop.tmp module.prop

echo "[*] Creating Magisk Module Zip: ${ZIP_NAME}..."
rm -f "$ZIP_NAME"
zip -r -o -X -ll "$ZIP_NAME" . \
  -x '.git/*' \
  -x 'build.sh' \
  -x 'module.prop.bak' \
  -x 'module.prop.tmp' \
  -x 'MonoBox*.zip' \
  -x 'webui/*' \
  -x '.github/*' \
  -x '.gitignore' \
  -x '.*/*' \
  -x '.*'

echo "[*] Cleaning up temporary files..."
mv -f module.prop.bak module.prop
rm -rf webroot

echo "[+] Done! Build generated at: ${ZIP_NAME}"
if [ -n "$GITHUB_OUTPUT" ]; then
    echo "version_code=${VERSION_CODE}" >> "$GITHUB_OUTPUT"
fi
