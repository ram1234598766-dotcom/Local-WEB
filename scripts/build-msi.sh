#!/usr/bin/env bash
# Build the Windows Installer (MSI) from the current source.
#
# Stages the payload that localweb.wxs references, then runs candle and light.
# The wintun driver is fetched and verified first, because the MSI bundles it.
#
# Requires: go, a WiX 3.x toolset, and network access on the first run.
# Run scripts/fetch-wintun.sh on its own if you only need the driver.
#
# Usage: scripts/build-msi.sh [output-directory]
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=scripts/windows-toolchain.sh
. "${REPO_ROOT}/scripts/windows-toolchain.sh"

OUT_DIR="${1:-${REPO_ROOT}/dist}"
OUT_DIR="$(normalize_out_dir "${OUT_DIR}")"
VERSION="${VERSION:-1.0.1}"

# Product metadata, overridable the same way VERSION is. UpgradeCode is fixed on
# purpose: it is what tells Windows a later build is the same product and should
# upgrade in place rather than install alongside.
PRODUCT_NAME="${PRODUCT_NAME:-LocalWEB}"
MANUFACTURER="${MANUFACTURER:-LocalWEB Project}"
UPGRADE_CODE="${UPGRADE_CODE:-{12345678-1234-1234-1234-123456789012}}"

# MSI Version is three numeric fields, so a pre-release suffix such as 1.1.0-rc1
# cannot be stamped and would be silently mangled. Fail rather than ship a package
# whose stamped version is not the one that was asked for.
if ! printf '%s' "${VERSION}" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
  echo "error: VERSION='${VERSION}' is not major.minor.build, which MSI Version requires" >&2
  exit 1
fi
STAGE="$(make_stage_dir "${OUT_DIR}/.stage")"
trap 'rm -rf "${STAGE}"' EXIT

# ---------------------------------------------------------------- locate ----

if ! CANDLE="$(find_build_tool candle "WiX Toolset v3.14/bin/candle.exe" "WiX*/bin/candle.exe")"; then
  cat >&2 <<'MSG'
error: candle not found.

candle and light ship with the WiX 3.x toolset. A copy of the installer is
committed in this repository, so you can either:

  installers/windows/wix314.exe      (double-click to install, then re-run)

or install WiX Toolset 3.14 from https://wixtoolset.org/ and make sure
candle.exe and light.exe are on PATH.
MSG
  exit 1
fi

if ! LIGHT="$(find_build_tool light "WiX Toolset v3.14/bin/light.exe" "WiX*/bin/light.exe")"; then
  echo "error: light not found. candle was found at ${CANDLE}, so the WiX" >&2
  echo "       install looks incomplete; reinstall the toolset." >&2
  exit 1
fi

echo "==> toolchain"
echo "    candle: ${CANDLE}"
echo "    light:  ${LIGHT}"

# ------------------------------------------------------------------ build ---

echo "==> wintun driver"
"${REPO_ROOT}/scripts/fetch-wintun.sh"

echo "==> windows binaries"
( cd "${REPO_ROOT}" && GOOS=windows GOARCH=amd64 go build -trimpath -o "${STAGE}/localweb.exe"      ./cmd/node )
( cd "${REPO_ROOT}" && GOOS=windows GOARCH=amd64 go build -trimpath -o "${STAGE}/localweb-cli.exe" ./cmd/cli )

echo "==> staging payload"
mkdir -p "${STAGE}/config" "${STAGE}/wintun"
cp -f "${REPO_ROOT}/README.md"          "${STAGE}/README.md"
cp -f "${REPO_ROOT}/CHANGELOG.md"       "${STAGE}/CHANGELOG.md"
cp -f "${REPO_ROOT}/LICENSE"                               "${STAGE}/LICENSE"
cp -f "${REPO_ROOT}/installers/windows/ServiceInstall.ps1" "${STAGE}/ServiceInstall.ps1"
cp -f "${REPO_ROOT}/installers/windows/config/config.json" "${STAGE}/config/config.json"
cp -f "${REPO_ROOT}/installers/windows/localweb.wxs"       "${STAGE}/localweb.wxs"
# the wxs references wintun\wintun.dll, which is gitignored and fetched above
cp -f "${REPO_ROOT}/installers/windows/wintun/wintun.dll"  "${STAGE}/wintun/wintun.dll"

# WSL launches a native Windows .exe from its Linux-style path, so ${CANDLE}
# is invoked as-is; only the file arguments are translated, because Windows
# cannot resolve a /mnt/c/... path.
WXS_ARG="$(to_tool_path "${CANDLE}" "${STAGE}/localweb.wxs")"
OBJ_ARG="$(to_tool_path "${CANDLE}" "${STAGE}/localweb.wixobj")"

echo "==> candle"
# Product metadata is passed here rather than left to the <?define ?> block in the
# wxs. Without it the output is named localweb_1.1.0_...msi while the package inside
# still reports 1.0.1, so Windows treats two different builds as the same product and
# an upgrade silently does nothing. One VERSION now drives the filename and the
# stamped ProductVersion together.
"${CANDLE}" -nologo -arch x64 -out "${OBJ_ARG}" -ext WixUtilExtension \
  "-dProductName=${PRODUCT_NAME}" \
  "-dProductVersion=${VERSION}" \
  "-dManufacturer=${MANUFACTURER}" \
  "-dUpgradeCode=${UPGRADE_CODE}" \
  "${WXS_ARG}"

echo "==> light"
mkdir -p "${OUT_DIR}"
MSI="${OUT_DIR}/localweb_${VERSION}_x64_en-US.msi"
rm -f "${MSI}"
"${LIGHT}" -nologo -ext WixUtilExtension -out "$(to_tool_path "${LIGHT}" "${MSI}")" "${OBJ_ARG}"

[ -f "${MSI}" ] || { echo "error: light produced no MSI at ${MSI}" >&2; exit 1; }
echo "==> built ${MSI}"
ls -l "${MSI}"
