#!/bin/sh
# Build reproducible pure-Go executables and verify the ELF has no dynamic
# interpreter or dependencies. This script can run on any Go build host.
set -eu

gateway_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$gateway_root"
gateway_out=${1:-dist/gateway}
gateway_version=${VERSION:-dev}
gateway_commit=$(git rev-parse --short=12 HEAD 2>/dev/null || printf unknown)
mkdir -p "$gateway_out"
for gateway_arch in amd64 arm64; do
    gateway_binary="$gateway_out/smartstage-gateway-linux-$gateway_arch"
    CGO_ENABLED=0 GOOS=linux GOARCH="$gateway_arch" \
        go build -mod=vendor -trimpath -buildvcs=false \
        -ldflags="-s -w -buildid= -X main.version=$gateway_version -X main.commit=$gateway_commit" \
        -o "$gateway_binary" ./cmd/smartstage-gateway
    if command -v readelf >/dev/null 2>&1; then
        if readelf -l "$gateway_binary" | grep -q INTERP; then
            printf 'Unexpected dynamic interpreter: %s\n' "$gateway_binary" >&2; exit 1
        fi
        if readelf -d "$gateway_binary" | grep -q NEEDED; then
            printf 'Unexpected dynamic dependency: %s\n' "$gateway_binary" >&2; exit 1
        fi
    else
        printf 'readelf is required to verify the static gateway binaries.\n' >&2; exit 1
    fi
    {
        printf 'Smart Stage Gateway %s (%s), linux/%s\n' "$gateway_version" "$gateway_commit" "$gateway_arch"
        printf 'CGO_ENABLED=0; no ELF INTERP or NEEDED entries.\n'
        go version -m "$gateway_binary"
        readelf -h "$gateway_binary"
        readelf -l "$gateway_binary"
        readelf -d "$gateway_binary"
    } > "$gateway_binary.static.txt"
    if command -v sha256sum >/dev/null 2>&1; then
        (cd "$gateway_out" && sha256sum "smartstage-gateway-linux-$gateway_arch" > "smartstage-gateway-linux-$gateway_arch.sha256")
    else
        (cd "$gateway_out" && shasum -a 256 "smartstage-gateway-linux-$gateway_arch" > "smartstage-gateway-linux-$gateway_arch.sha256")
    fi
done
cp install-gateway.sh "$gateway_out/install-gateway.sh"
