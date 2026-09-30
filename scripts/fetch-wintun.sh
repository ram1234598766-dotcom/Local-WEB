#!/usr/bin/env bash
# Fetch and verify the Wintun kernel driver used by the Windows VPN service.
#
# wintun.dll is a binary dependency and is deliberately not committed: *.dll is
# gitignored so the repo stays free of opaque executables. Without this script a
# fresh clone cannot build the MSI or the NSIS installer, because both bundle
# the driver.
#
# The download is pinned by SHA-256 and the checksum is verified before
# anything is written into the source tree. A mismatch is a hard failure: this
# is a kernel driver that gets installed into System32\drivers, so an
# unverified copy is not acceptable.
#
# Usage: scripts/fetch-wintun.sh [--force]
set -euo pipefail

WINTUN_VERSION="0.14.1"
WINTUN_URL="https://www.wintun.net/builds/wintun-${WINTUN_VERSION}.zip"
# SHA-256 of wintun-0.14.1.zip as published by wintun.net.
WINTUN_SHA256="07c256185d6ee3652e09fa55c0b673e2624b565e02c4b9091c79ca7d2f24ef51"

# SHA-256 of the amd64 wintun.dll inside that archive, checked after extraction
# so a repackaged archive with the same name cannot substitute a different
# driver.
WINTUN_DLL_SHA256="e5da8447dc2c320edc0fc52fa01885c103de8c118481f683643cacc3220dafce"
WINTUN_DLL_SIZE="427552"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEST_DIR="${REPO_ROOT}/installers/windows/wintun"
DEST_DLL="${DEST_DIR}/wintun.dll"

FORCE=0
[ "${1:-}" = "--force" ] && FORCE=1

# ---------------------------------------------------------------- helpers ---

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    echo "error: neither sha256sum nor shasum is available" >&2
    return 1
  fi
}

fetch() {
  local url="$1" out="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --retry 3 --retry-delay 2 -o "$out" "$url"
  elif command -v wget >/dev/null 2>&1; then
    wget -q --tries=3 -O "$out" "$url"
  else
    echo "error: neither curl nor wget is available" >&2
    return 1
  fi
}

# ------------------------------------------------------------------- main ---

if [ -f "${DEST_DLL}" ] && [ "${FORCE}" -eq 0 ]; then
  have="$(sha256_of "${DEST_DLL}" || true)"
  if [ "${have}" = "${WINTUN_DLL_SHA256}" ]; then
    echo "wintun ${WINTUN_VERSION}: already present and verified (${DEST_DLL})"
    exit 0
  fi
  echo "wintun ${WINTUN_VERSION}: present but checksum differs, refetching"
fi

TMPDIR="$(mktemp -d)"
trap 'rm -rf "${TMPDIR}"' EXIT

ZIP="${TMPDIR}/wintun-${WINTUN_VERSION}.zip"
echo "wintun ${WINTUN_VERSION}: downloading ${WINTUN_URL}"
fetch "${WINTUN_URL}" "${ZIP}"

got="$(sha256_of "${ZIP}")"
if [ "${got}" != "${WINTUN_SHA256}" ]; then
  echo "error: archive checksum mismatch for ${WINTUN_URL}" >&2
  echo "  expected ${WINTUN_SHA256}" >&2
  echo "  actual   ${got}" >&2
  echo "refusing to continue: this file is installed as a kernel driver" >&2
  exit 1
fi
echo "  archive checksum OK"

unzip -q -o "${ZIP}" -d "${TMPDIR}/x"

# The archive ships one wintun.dll per architecture. Select the amd64 build by
# size rather than by path, because the directory layout is not contractual.
dll=""
for candidate in $(find "${TMPDIR}/x" -name 'wintun.dll' -type f); do
  size="$(wc -c < "${candidate}" | tr -d ' ')"
  if [ "${size}" = "${WINTUN_DLL_SIZE}" ]; then
    dll="${candidate}"
    break
  fi
done

if [ -z "${dll}" ]; then
  echo "error: no ${WINTUN_DLL_SIZE}-byte wintun.dll (amd64) inside the archive" >&2
  exit 1
fi

got="$(sha256_of "${dll}")"
if [ "${got}" != "${WINTUN_DLL_SHA256}" ]; then
  echo "error: extracted wintun.dll checksum mismatch" >&2
  echo "  expected ${WINTUN_DLL_SHA256}" >&2
  echo "  actual   ${got}" >&2
  exit 1
fi

mkdir -p "${DEST_DIR}"
cp -f "${dll}" "${DEST_DLL}"

# LICENSE is already committed alongside the DLL, so only restore it if it has
# gone missing. wintun.dll.sig is likewise committed and is deliberately not
# restored here: wintun.net does not publish a .sig for the archive, so there
# is nothing to re-fetch it from. scripts/build-nsis.sh fails loudly if it is
# absent rather than producing an installer without it.
if [ ! -f "${DEST_DIR}/LICENSE" ]; then
  lic="$(find "${TMPDIR}/x" -name 'LICENSE*' -type f | head -n 1 || true)"
  [ -n "${lic}" ] && cp -f "${lic}" "${DEST_DIR}/LICENSE"
fi

echo "  extracted and verified: ${DEST_DLL}"
echo "wintun ${WINTUN_VERSION}: ready"
