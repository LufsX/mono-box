#!/usr/bin/env sh
set -e

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

echo "[*] Creating Magisk Module Zip: ${ZIP_NAME}..."
rm -f "$ZIP_NAME"
zip -r -o -X -ll "$ZIP_NAME" . \
  -x '.git/*' \
  -x 'build.sh' \
  -x '.github/*' \
  -x '.gitignore' \
  -x 'webui/*' \
  -x '.*/*' \
  -x '.*'

echo "[*] Cleaning up temporary files..."
rm -rf webroot

echo "[+] Done! Build generated at: ${ZIP_NAME}"
