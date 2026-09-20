#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

target_os=${1:-$(go env GOOS)}
target_arch=${2:-$(go env GOARCH)}
build_version=${VERSION:-dev}
build_commit=$(git rev-parse --short=12 HEAD 2>/dev/null || printf unknown)
export GOOS="$target_os" GOARCH="$target_arch" CGO_ENABLED=1
suffix=""
case "$target_os/$target_arch" in
  windows/amd64|windows/arm64)
    suffix=.exe
    triple=x86_64
    if [[ "$target_arch" == arm64 ]]; then triple=aarch64; fi
    export CC=${CC:-$triple-w64-mingw32-clang}
    export CXX=${CXX:-$triple-w64-mingw32-clang++}
    ;;
  darwin/amd64|darwin/arm64)
    [[ $(uname -s) == Darwin ]] || { echo 'macOS builds require an Apple SDK on a Mac.' >&2; exit 1; }
    export CC=${CC:-clang} CXX=${CXX:-clang++}
    export MACOSX_DEPLOYMENT_TARGET=12.0
    ;;
  *) echo "Unsupported release target: $target_os/$target_arch" >&2; exit 1 ;;
esac
mkdir -p dist
if [[ "$target_os" == windows ]]; then
  resource="cmd/smartstage/icon_windows_$target_arch.syso"
  trap 'rm -f -- "$resource"' EXIT
  "$triple-w64-mingw32-windres" --input packaging/windows/smartstage.rc --output "$resource" --output-format coff --include-dir .
fi
record="dist/toolchain-$target_os-$target_arch.txt"
{
  go version
  "$CC" --version
  git rev-parse HEAD
  if [[ "$target_os" == darwin ]]; then xcodebuild -version; xcrun --show-sdk-version; sw_vers; fi
} > "$record"

commands=(native-harness)
if [[ -d cmd/smartstage ]]; then commands+=(smartstage); fi
for command in "${commands[@]}"; do
  artifact="dist/$command-$target_os-$target_arch$suffix"
  flags="-X main.version=$build_version -X main.commit=$build_commit"
  if [[ "$target_os" == windows && "$command" == smartstage ]]; then flags="-H windowsgui $flags"; fi
  if [[ ${DEBUG:-0} != 1 ]]; then flags="-s -w $flags"; fi
  go build -trimpath -buildvcs=true -ldflags "$flags" -o "$artifact" "./cmd/$command"
  if [[ "$target_os" == darwin ]]; then codesign --force --sign - "$artifact"; fi
  python3 scripts/audit-dependencies.py "$artifact"
  python3 - "$artifact" <<'PY'
import hashlib, pathlib, sys
p = pathlib.Path(sys.argv[1])
p.with_name(p.name + '.sha256').write_text(hashlib.sha256(p.read_bytes()).hexdigest() + '  ' + p.name + '\n')
PY
done

python3 scripts/package-release.py "$target_os" "$target_arch"

if [[ "$target_os" == darwin ]]; then
  python3 scripts/package-macos.py "$target_arch" "$build_version"
fi
