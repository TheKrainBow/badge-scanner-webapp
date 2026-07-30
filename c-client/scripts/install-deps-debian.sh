#!/usr/bin/env bash
#
# Install everything needed to build the badge-scanner C client on
# Debian/Ubuntu (and derivatives: Mint, Pop!_OS, Raspberry Pi OS, ...).
#
#   ./scripts/install-deps-debian.sh          # build + GUI deps
#
# After this, from webapp/c-client/:
#   make        # CLI  -> bin/badge-lookup
#   make gui    # GUI  -> bin/badge-lookup-gui   (needs libgtk-3-dev, installed below)
#
set -euo pipefail

if ! command -v apt-get >/dev/null 2>&1; then
    echo "This script needs apt-get (Debian/Ubuntu family). For Fedora use install-deps-fedora.sh." >&2
    exit 1
fi

SUDO=""
if [ "$(id -u)" -ne 0 ]; then
    SUDO="sudo"
fi

# Build toolchain + library dev headers.
#   build-essential  -> gcc + make
#   pkg-config       -> how the Makefile finds libcurl/PC/SC/GTK
#   libcurl4-openssl-dev -> HTTP + WebSocket lookups
#   libpcsclite-dev  -> PC/SC reader access (winscard.h + libpcsclite.pc)
#   libgtk-3-dev     -> the optional `make gui` graphical client
# cJSON is vendored in src/vendor, so no package is needed for it.
PACKAGES=(
    build-essential
    pkg-config
    libcurl4-openssl-dev
    libpcsclite-dev
    libgtk-3-dev
)

# Runtime pieces that actually talk to a physical reader. Not required to
# *compile*, but you'll want them to run against real hardware.
RUNTIME=(
    pcscd
    libccid
)

echo "==> Updating package lists"
$SUDO apt-get update

echo "==> Installing build + GUI dependencies:"
printf '    %s\n' "${PACKAGES[@]}"
$SUDO apt-get install -y "${PACKAGES[@]}"

echo "==> Installing PC/SC runtime (reader daemon + CCID driver):"
printf '    %s\n' "${RUNTIME[@]}"
$SUDO apt-get install -y "${RUNTIME[@]}"

echo "==> Enabling and starting pcscd (the PC/SC daemon):"
$SUDO systemctl enable --now pcscd 2>/dev/null || \
    echo "    (couldn't enable pcscd automatically — start it yourself if reads fail)"

echo
echo "Done. Build with:  make   (CLI)   or   make gui   (GUI)"
