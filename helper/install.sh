#!/usr/bin/env bash
#
# Put caddy-reader on this machine.
#
#    curl -fsSL https://<your-homehub>/helper/install.sh | sudo bash
#
# This only installs the binary. It starts nothing and changes nothing: setting
# up the service is `caddy-reader setup`, which asks first.
#
set -euo pipefail

# Where to download from. When this script is served by a HomeHub install,
# that install substitutes its own address here, so the whole setup works on a
# LAN with no GitHub in the path. The literal below is the fallback for running
# the script straight from the repository.
BASE_URL="${CADDY_READER_BASE_URL:-__BASE_URL__}"
REPO="TheOriUHD/HomeHub"
DEST="/usr/local/bin/caddy-reader"

bold=$(tput bold 2>/dev/null || true)
green=$(tput setaf 2 2>/dev/null || true)
dim=$(tput setaf 8 2>/dev/null || true)
off=$(tput sgr0 2>/dev/null || true)

die() { printf '\n  %s\n\n' "$1" >&2; exit 1; }

[ "$(id -u)" = "0" ] || die "Run this with sudo — it writes to ${DEST}."

case "$(uname -m)" in
  x86_64|amd64)  ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  armv7l|armv7)  ARCH=armv7 ;;
  *) die "No build for $(uname -m). Build from source: see helper/README.md" ;;
esac

printf '\n  %scaddy-reader%s\n  %sHomeHub'"'"'s window onto your Caddy%s\n\n' "$bold" "$off" "$dim" "$off"
printf '  Downloading the %s build…\n' "$ARCH"

if [ "$BASE_URL" != "__BASE_URL__" ]; then
  URL="${BASE_URL%/}/helper/caddy-reader-linux-${ARCH}"
else
  URL="https://github.com/${REPO}/releases/latest/download/caddy-reader-linux-${ARCH}"
fi
TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT

curl -fsSL "$URL" -o "$TMP" || die "Could not download ${URL}
  Is there a release yet? Releases are cut by tagging: helper-vX.Y.Z"
chmod +x "$TMP"

# Check it runs before replacing anything, so a bad download cannot take out a
# working install.
"$TMP" version >/dev/null 2>&1 || die "That download does not run on this machine."
VERSION="$("$TMP" version)"

# A running binary cannot be overwritten in place; moving it replaces the
# directory entry instead, which works while the service is up.
mv "$TMP" "$DEST"
trap - EXIT
chmod 755 "$DEST"

if systemctl is-active --quiet caddy-reader 2>/dev/null; then
  systemctl restart caddy-reader
  printf '\n  %s✓%s  Updated to %s and restarted.\n\n' "$green" "$off" "$VERSION"
  printf '  %scaddy-reader status%s\n\n' "$bold" "$off"
  exit 0
fi

printf '\n  %s✓%s  Installed %s\n\n' "$green" "$off" "$VERSION"
printf '  Now set it up:\n\n'
printf '      %ssudo caddy-reader setup%s\n\n' "$bold" "$off"
printf '  %sIt asks a few questions, all with sensible answers already filled in,%s\n' "$dim" "$off"
printf '  %sand ends with a six-digit code to pair with HomeHub.%s\n\n' "$dim" "$off"
