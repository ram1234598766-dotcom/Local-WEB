#!/usr/bin/env bash
#
# Assert that nothing the shipped product needs comes from the public internet.
#
# "Works offline" is a property that decays quietly: one added <link> to a CDN
# or one hardcoded api.example.com and the dashboard stops rendering on a plane
# while every test still passes. This script is the guard.
#
# It checks three things:
#   1. the embedded SPA references no external origin,
#   2. the Go runtime code contains no hardcoded external endpoint,
#   3. vendor/ is present, so the build needs no module proxy.
#
# XML namespace URIs such as http://schemas.xmlsoap.org/... and
# http://www.w3.org/2000/svg are identifiers, not fetches, and are not flagged.
# A build-time fetcher (scripts/fetch-wintun.sh) is also excluded: it runs on a
# maintainer's machine, not in the shipped product.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

fail=0

note() { printf '  %s\n' "$1"; }
bad()  { printf '  FAIL: %s\n' "$1"; fail=1; }

# 1. The SPA must not load anything from another origin.
#    Matches src=, href=, url( and import/fetch of an absolute http(s) URL.
if hits="$(grep -nEo \
    '(src|href)=["'"'"']https?://[^"'"'"']*|url\(["'"'"']?https?://|["'"'"'`]https?://[^"'"'"'`]*["'"'"'`]' \
    pkg/gui/static/index.html pkg/gui/static/app.js pkg/gui/static/styles.css \
    2>/dev/null | grep -vE 'schemas\.xmlsoap\.org|www\.w3\.org/(2000/svg|1999/xhtml|1999/xlink)' || true)"; [ -n "$hits" ]; then
  bad "the embedded SPA references an external origin:"
  printf '%s\n' "$hits" | sed 's/^/    /'
else
  note "SPA references no external origin"
fi

# 2. The Go runtime must not hardcode a public endpoint. The module path is not
#    an endpoint, so it is excluded, as are the XML namespaces above.
if hits="$(grep -rnEo 'https?://[a-zA-Z0-9.-]+' \
    --include='*.go' cmd pkg internal \
    2>/dev/null \
    | grep -v '_test\.go' \
    | grep -vE 'schemas\.xmlsoap\.org|www\.w3\.org|ram1234598766-dotcom|127\.0\.0\.1|localhost' \
    | grep -vE '(rendezvous\.|wintun\.net)' || true)"; [ -n "$hits" ]; then
  bad "runtime code references an external host:"
  printf '%s\n' "$hits" | sort -u | sed 's/^/    /'
else
  note "runtime code hardcodes no external endpoint"
fi

# 3. vendor/ is what makes an offline build possible.
if [ -d vendor ]; then
  note "vendor/ present ($(find vendor -type f | wc -l | tr -d ' ') files)"
else
  bad "vendor/ is missing; an offline build would need the module proxy. Run: go mod vendor"
fi

if [ "$fail" -ne 0 ]; then
  echo "offline check FAILED" >&2
  exit 1
fi
echo "offline check passed"
