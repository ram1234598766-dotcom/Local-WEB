#!/usr/bin/env bash
# Build the NSIS (.exe) installer from the current source.
#
# Stages what localweb.nsi references and runs makensis. The output name is
# declared inside the .nsi (Name / OutFile) rather than passed on the command
# line, so the built artefact is copied out afterwards.
#
# Requires: go, NSIS 3.x, and network access on the first run.
#
# Usage: scripts/build-nsis.sh [output-directory]
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=scripts/windows-toolchain.sh
. "${REPO_ROOT}/scripts/windows-toolchain.sh"

OUT_DIR="${1:-${REPO_ROOT}/dist}"
OUT_DIR="$(normalize_out_dir "${OUT_DIR}")"
STAGE="$(make_stage_dir "${OUT_DIR}/.stage")"
trap 'rm -rf "${STAGE}"' EXIT

# ---------------------------------------------------------------- locate ----

if ! MAKENSIS="$(find_build_tool makensis "NSIS/makensis.exe")"; then
  cat >&2 <<'MSG'
error: makensis not found.

Install NSIS 3.x from https://nsis.sourceforge.io/ and make sure makensis is
on PATH, or set MAKENSIS_EXE to the full path of makensis.exe.
MSG
  exit 1
fi

echo "==> toolchain"
echo "    makensis: ${MAKENSIS}"

# ------------------------------------------------------------------ build ---

echo "==> wintun driver"
"${REPO_ROOT}/scripts/fetch-wintun.sh"

echo "==> windows binaries"
( cd "${REPO_ROOT}" && GOOS=windows GOARCH=amd64 go build -trimpath -o "${STAGE}/localweb.exe"      ./cmd/node )
( cd "${REPO_ROOT}" && GOOS=windows GOARCH=amd64 go build -trimpath -o "${STAGE}/localweb-cli.exe" ./cmd/cli )

echo "==> staging payload"
cp -f "${REPO_ROOT}/installers/windows/README.md"     "${STAGE}/README.md"
cp -f "${REPO_ROOT}/installers/windows/CHANGELOG.md"  "${STAGE}/CHANGELOG.md"
cp -f "${REPO_ROOT}/installers/windows/LICENSE"       "${STAGE}/LICENSE"
cp -f "${REPO_ROOT}/installers/windows/localweb.nsi"  "${STAGE}/localweb.nsi"
cp -f "${REPO_ROOT}/installers/windows/CHANGELOG.md"  "${STAGE}/CHANGELOG.md"

# The nsi embeds the driver's signature, which is committed to the repository
# (wintun.net does not publish a .sig, so it cannot be re-fetched).
SIG="${REPO_ROOT}/installers/windows/wintun/wintun.dll.sig"
[ -f "${SIG}" ] || {
  echo "error: ${SIG} is missing from the working tree." >&2
  echo "       It is tracked in git; run 'git checkout -- installers/windows/wintun'." >&2
  exit 1
}

mkdir -p "${STAGE}/wintun" "${STAGE}/scripts" "${STAGE}/config"
cp -f "${REPO_ROOT}/installers/windows/wintun/wintun.dll"    "${STAGE}/wintun/wintun.dll"
cp -f "${SIG}"                                                 "${STAGE}/wintun/wintun.dll.sig"
cp -f "${REPO_ROOT}/installers/windows/wintun/LICENSE"        "${STAGE}/wintun/LICENSE"
cp -f "${REPO_ROOT}/installers/windows/scripts/"*.ps1         "${STAGE}/scripts/"
cp -f "${REPO_ROOT}/installers/windows/config/"*.json         "${STAGE}/config/"

echo "==> makensis"
# OutFile is relative to the working directory, so run from the staging dir.
# ${MAKENSIS} is invoked as-is: WSL launches a native Windows exe from its
# Linux-style path. to_tool_path picks the right argument style for whichever
# makensis was found, Linux or Windows.
( cd "${STAGE}" && "${MAKENSIS}" "$(to_tool_path "${MAKENSIS}" "${STAGE}/localweb.nsi")" )

mkdir -p "${OUT_DIR}"
built="$(find "${STAGE}" -maxdepth 1 -name '*-setup.exe' | head -n 1)"
[ -n "${built}" ] || { echo "error: makensis produced no *-setup.exe" >&2; exit 1; }
cp -f "${built}" "${OUT_DIR}/$(basename "${built}")"
echo "==> built ${OUT_DIR}/$(basename "${built}")"
ls -l "${OUT_DIR}/$(basename "${built}")"
