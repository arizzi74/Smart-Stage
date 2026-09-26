#!/bin/sh
# All work lives in main so an interrupted script download cannot begin setup.
set -eu

fail() { printf 'Smart Stage Gateway: %s\n' "$*" >&2; exit 1; }

main() {
    [ "$(uname -s)" = Linux ] || fail 'This installer supports Linux only.'
    case "$(uname -m)" in
        x86_64|amd64) gateway_arch=amd64 ;;
        aarch64|arm64) gateway_arch=arm64 ;;
        *) fail 'Supported Linux architectures are amd64 and arm64.' ;;
    esac
    command -v curl >/dev/null 2>&1 || fail 'curl is required.'
    command -v sha256sum >/dev/null 2>&1 || fail 'sha256sum is required.'
    # This script is published alongside this exact release. Do not fetch an
    # independently changing latest binary during an installation.
    gateway_version=v1.2.0
    gateway_asset="smartstage-gateway-linux-$gateway_arch"
    gateway_base="https://github.com/arizzi74/Smart-Stage/releases/download/$gateway_version"
    gateway_temp=$(mktemp -d) || fail 'Cannot create a temporary directory.'
    trap 'rm -rf "$gateway_temp"' EXIT HUP INT TERM
    printf 'Downloading Smart Stage Gateway %s for Linux %s…\n' "$gateway_version" "$gateway_arch"
    curl --fail --show-error --silent --location --proto '=https' --tlsv1.2 \
        "$gateway_base/$gateway_asset" -o "$gateway_temp/$gateway_asset"
    curl --fail --show-error --silent --location --proto '=https' --tlsv1.2 \
        "$gateway_base/$gateway_asset.sha256" -o "$gateway_temp/checksum"
    gateway_expected=$(awk 'NR == 1 { print $1 }' "$gateway_temp/checksum")
    [ "${#gateway_expected}" -eq 64 ] || fail 'Invalid release checksum.'
    case "$gateway_expected" in *[!0-9a-f]*) fail 'Invalid release checksum.' ;; esac
    gateway_actual=$(sha256sum "$gateway_temp/$gateway_asset")
    gateway_actual=${gateway_actual%% *}
    [ "$gateway_actual" = "$gateway_expected" ] || fail 'The downloaded executable failed SHA-256 verification.'
    chmod 700 "$gateway_temp/$gateway_asset"
    if [ "$(id -u)" -eq 0 ]; then
        "$gateway_temp/$gateway_asset" install "$@"
    else
        command -v sudo >/dev/null 2>&1 || fail 'sudo is required, or run this command as root.'
        sudo "$gateway_temp/$gateway_asset" install "$@"
    fi
}

main "$@"
