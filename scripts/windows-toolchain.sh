#!/usr/bin/env bash
# Locate a Windows build tool from a bash shell that may be either Git Bash or
# WSL. Sourced by scripts/build-msi.sh and scripts/build-nsis.sh.
#
# Handles three things that differ between the two shells:
#   * drive mount prefix  - Git Bash uses /c, WSL uses /mnt/c
#   * spaces in "Program Files (x86)" without letting the shell word-split
#   * /tmp is not visible to a native Windows .exe under WSL, so any path
#     handed to candle/light/makensis is converted with wslpath first

# Print the tool's path on stdout, or return 1 if it cannot be found.
find_build_tool() {
  local name="$1"; shift
  # Explicit override always wins.
  local override_var="${name^^}_EXE"
  override_var="${override_var//-/_}_EXE"
  local override="${!override_var:-}"
  if [ -n "${override}" ] && [ -e "${override}" ]; then
    echo "${override}"; return 0
  fi
  if command -v "${name}" >/dev/null 2>&1; then
    command -v "${name}"; return 0
  fi
  # Windows install locations, both mount styles. The escaped space keeps this
  # a single word while leaving the trailing * active as a glob.
  local root
  for root in /c /mnt/c; do
    local candidate
    for candidate in "$root"/Program\ Files*/"$@"; do
      if [ -f "${candidate}" ]; then echo "${candidate}"; return 0; fi
    done
  done
  return 1
}

# Convert a path so that the given tool can open it.
#
# A native Windows executable cannot resolve a /mnt/c/... path, so those get
# translated with wslpath. A native Linux executable (NSIS is commonly
# installed as a Linux package inside WSL) needs the Linux path untouched, so
# converting its arguments would break it. Decide from the tool's own location.
to_tool_path() {
  local tool="$1" p="$2"
  if [[ "${tool}" =~ ^[A-Za-z]:[\\/] ]] \
     || [[ "${tool}" == /c/* ]] || [[ "${tool}" == /mnt/c/* ]] \
     || [[ "${tool}" == *.exe ]]; then
    if command -v wslpath >/dev/null 2>&1; then
      wslpath -w "${p}" 2>/dev/null || echo "${p}"
      return 0
    fi
  fi
  echo "${p}"
}

# Accept a Windows-style absolute path (C:\foo\bar) from a PowerShell caller and
# turn it into something the shell can actually use. Without this, a backslash
# path handed to mkdir becomes one long mangled word.
normalize_out_dir() {
  local p="${1:-}"
  if [ -z "${p}" ]; then
    echo ""; return 0
  fi
  if [[ "${p}" =~ ^[A-Za-z]:[\\/] ]]; then
    if command -v wslpath >/dev/null 2>&1; then
      wslpath -u "${p}"; return 0
    fi
    # No wslpath means Git Bash, where /c/foo/bar is the equivalent.
    local rest="${p#?:}"
    echo "/c/${rest//\\//}"
    return 0
  fi
  echo "${p}"
}

# Create a staging directory that is visible to native Windows tools.
# /tmp is not, so stage inside the repository instead.
#
# The result is always absolute. The build scripts cd into the staging
# directory and then hand the tool a path inside it, so a relative stage path
# (which is what `make nsis` produces, because DISTDIR defaults to "dist")
# would no longer resolve.
make_stage_dir() {
  local parent="$1"; shift
  mkdir -p "${parent}"
  local d
  d="$(mktemp -d "${parent}/stage-XXXXXX")"
  case "${d}" in
    /*) echo "${d}" ;;
    *)  echo "${PWD}/${d}" ;;
  esac
}
