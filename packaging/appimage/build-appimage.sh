#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:?version required}"
ARCH="${2:?arch required (x86_64 or aarch64)}"
BIN_PATH="${3:-./dist/myovpn}"

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
APPDIR="${ROOT_DIR}/build/AppDir-${ARCH}"
rm -rf "${APPDIR}"
mkdir -p "${APPDIR}/usr/bin" "${APPDIR}/usr/share/applications" "${APPDIR}/usr/share/icons/hicolor/256x256/apps"

cp "${BIN_PATH}" "${APPDIR}/usr/bin/myovpn"
chmod 755 "${APPDIR}/usr/bin/myovpn"
cp "${ROOT_DIR}/packaging/linux/myovpn.desktop" "${APPDIR}/myovpn.desktop"
sed -i 's|^Exec=/usr/bin/myovpn|Exec=myovpn|' "${APPDIR}/myovpn.desktop"
cp "${APPDIR}/myovpn.desktop" "${APPDIR}/usr/share/applications/myovpn.desktop"
cp "${ROOT_DIR}/assets/icons/myovpn-brand-256.png" "${APPDIR}/myovpn.png"
cp "${ROOT_DIR}/assets/icons/myovpn-brand-256.png" "${APPDIR}/usr/share/icons/hicolor/256x256/apps/myovpn.png"

cat > "${APPDIR}/AppRun" <<'EOF'
#!/bin/sh
HERE="$(dirname "$(readlink -f "$0")")"
export PATH="${HERE}/usr/bin:${PATH}"
exec "${HERE}/usr/bin/myovpn" "$@"
EOF
chmod 755 "${APPDIR}/AppRun"

OUT_FILE="${ROOT_DIR}/dist/MyOVPN-${VERSION}-${ARCH}.AppImage"
mkdir -p "${ROOT_DIR}/dist"
ARCH="${ARCH}" appimagetool --appimage-extract-and-run "${APPDIR}" "${OUT_FILE}"
echo "Built ${OUT_FILE}"
