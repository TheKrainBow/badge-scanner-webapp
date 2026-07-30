#!/usr/bin/env bash
#
# Install everything needed to build the badge-scanner C client on Fedora
# (and RHEL/CentOS/Rocky/Alma with dnf).
#
#   ./scripts/install-deps-fedora.sh          # build + GUI deps
#
# After this, from webapp/c-client/:
#   make        # CLI  -> bin/badge-lookup
#   make gui    # GUI  -> bin/badge-lookup-gui   (needs gtk3-devel, installed below)
#
set -euo pipefail

if ! command -v dnf >/dev/null 2>&1; then
    echo "This script needs dnf (Fedora/RHEL family). For Debian/Ubuntu use install-deps-debian.sh." >&2
    exit 1
fi

SUDO=""
if [ "$(id -u)" -ne 0 ]; then
    SUDO="sudo"
fi

# Build toolchain + library dev headers.
#   gcc, make, pkgconf-pkg-config -> compiler / build / pkg-config
#   libcurl-devel                 -> HTTP + WebSocket lookups
#   pcsc-lite-devel               -> PC/SC reader access (winscard.h)
#   gtk3-devel                    -> the optional `make gui` graphical client
# cJSON is vendored in src/vendor, so no package is needed for it.
PACKAGES=(
    gcc
    make
    pkgconf-pkg-config
    libcurl-devel
    pcsc-lite-devel
    gtk3-devel
)

# Runtime pieces that actually talk to a physical reader. Not required to
# *compile*, but you'll want them to run against real hardware.
RUNTIME=(
    pcsc-lite
    pcsc-lite-ccid
)

echo "==> Installing build + GUI dependencies:"
printf '    %s\n' "${PACKAGES[@]}"
$SUDO dnf install -y "${PACKAGES[@]}"

echo "==> Installing PC/SC runtime (reader daemon + CCID driver):"
printf '    %s\n' "${RUNTIME[@]}"
$SUDO dnf install -y "${RUNTIME[@]}"

echo "==> Enabling and starting pcscd (the PC/SC daemon):"
$SUDO systemctl enable --now pcscd.socket 2>/dev/null || \
    echo "    (couldn't enable pcscd.socket automatically — start it yourself if reads fail)"

echo
echo "Done. Build with:  make   (CLI)   or   make gui   (GUI)"
