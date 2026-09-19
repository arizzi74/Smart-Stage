#!/bin/sh
# Smart Stage macOS installer. Run as your normal user; no sudo is needed.
set -eu
repo='arizzi74/Smart-Stage'
version=${SMARTSTAGE_VERSION:-v0.1.0-preview.3}
case "$version" in ''|*[!a-zA-Z0-9._-]*) echo 'Invalid SMARTSTAGE_VERSION' >&2; exit 1;; esac
if [ "$(uname -s)" != Darwin ]; then echo 'This installer supports macOS. Use install.ps1 on Windows.' >&2; exit 1; fi
case "$(uname -m)" in
  arm64) arch=arm64 ;;
  x86_64)
    arch=amd64
    if [ "$(sysctl -in hw.optional.arm64 2>/dev/null || true)" = 1 ]; then arch=arm64; fi
    ;;
  *) echo 'Supported Mac architectures: Apple Silicon and Intel 64-bit.' >&2; exit 1 ;;
esac
install_dir=${SMARTSTAGE_INSTALL_DIR:-"$HOME/.local/bin"}
mkdir -p "$install_dir"
temporary=$(mktemp -d "$install_dir/.smartstage-install.XXXXXX")
trap 'rm -rf "$temporary"' EXIT HUP INT TERM
asset="smartstage-darwin-$arch"
base="https://github.com/$repo/releases/download/$version"
echo "Downloading Smart Stage ${version} for macOS ${arch}..."
curl --proto '=https' --tlsv1.2 --fail --silent --show-error --location "$base/$asset" -o "$temporary/$asset"
curl --proto '=https' --tlsv1.2 --fail --silent --show-error --location "$base/$asset.sha256" -o "$temporary/checksum"
expected=$(awk 'NR==1 {print $1}' "$temporary/checksum")
actual=$(shasum -a 256 "$temporary/$asset" | awk '{print $1}')
if [ ${#expected} -ne 64 ] || [ "$actual" != "$expected" ]; then echo 'Checksum mismatch; nothing was installed.' >&2; exit 1; fi
chmod 755 "$temporary/$asset"
mv -f "$temporary/$asset" "$install_dir/smartstage"
if [ "$install_dir" = "$HOME/.local/bin" ]; then
  case "${SHELL:-/bin/zsh}" in
    */zsh) profile="$HOME/.zprofile" ;;
    */bash) profile="$HOME/.bash_profile" ;;
    *) profile='' ;;
  esac
  if [ -n "$profile" ]; then
    path_line='export PATH="$HOME/.local/bin:$PATH"'
    if ! grep -Fqx "$path_line" "$profile" 2>/dev/null; then
      printf '\n# Smart Stage user installation\n%s\n' "$path_line" >> "$profile"
    fi
  fi
fi
"$install_dir/smartstage" --version
printf '\nInstalled: %s/smartstage\nRun now: "%s/smartstage"\n' "$install_dir" "$install_dir"
echo 'In a new login terminal, run: smartstage'
echo 'This is a preview build. Physical routing, clean-machine acceptance and the two-hour soak remain unverified.'
echo 'The app prints Admin/Command URLs and separate pairing keys when started.'
