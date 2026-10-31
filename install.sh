#!/bin/sh
# Install veyra on macOS or Linux.
#
#   From a downloaded archive:   tar -xzf veyra_*.tar.gz && sh install.sh
#     (installs the veyra binary that sits next to this script; nothing is downloaded)
#   Without an archive:          curl -fsSL https://raw.githubusercontent.com/RishiKendai/veyra/main/install.sh | sh
#     (downloads the latest release and verifies its checksum)
#
# Set VEYRA_INSTALL_DIR to choose the folder, VEYRA_VERSION (e.g. v0.1.0) to pin a release.
set -eu

repo="RishiKendai/veyra"

here="$(cd "$(dirname "$0")" 2>/dev/null && pwd || true)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

if [ -n "$here" ] && [ -f "$here/veyra" ]; then
  # Running from an extracted archive: install the binary next to this script.
  src="$here/veyra"
else
  case "$(uname -s)" in
    Darwin) os=darwin ;;
    Linux) os=linux ;;
    *) echo "veyra: unsupported OS $(uname -s); on Windows use install.ps1" >&2; exit 1 ;;
  esac
  case "$(uname -m)" in
    x86_64 | amd64) arch=amd64 ;;
    arm64 | aarch64) arch=arm64 ;;
    *) echo "veyra: unsupported CPU $(uname -m)" >&2; exit 1 ;;
  esac

  if [ -n "${VEYRA_VERSION:-}" ]; then
    base="https://github.com/$repo/releases/download/$VEYRA_VERSION"
  else
    base="https://github.com/$repo/releases/latest/download"
  fi
  archive="veyra_${os}_${arch}.tar.gz"

  fetch() { # url dest
    if command -v curl >/dev/null 2>&1; then curl -fsSL "$1" -o "$2"
    elif command -v wget >/dev/null 2>&1; then wget -q "$1" -O "$2"
    else echo "veyra: curl or wget is required" >&2; exit 1; fi
  }

  echo "Downloading $archive ..."
  fetch "$base/$archive" "$tmp/$archive"
  fetch "$base/checksums.txt" "$tmp/checksums.txt"

  want="$(grep " $archive\$" "$tmp/checksums.txt" | awk '{print $1}')"
  if [ -z "$want" ]; then echo "veyra: no checksum listed for $archive" >&2; exit 1; fi
  if command -v sha256sum >/dev/null 2>&1; then got="$(sha256sum "$tmp/$archive" | awk '{print $1}')"
  else got="$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')"; fi
  if [ "$want" != "$got" ]; then echo "veyra: checksum mismatch, refusing to install" >&2; exit 1; fi

  tar -xzf "$tmp/$archive" -C "$tmp" veyra
  src="$tmp/veyra"
fi

on_path() { case ":$PATH:" in *":$1:"*) return 0 ;; esac; return 1; }

# Prefer a folder that is already on the PATH and writable, so `veyra` works at once.
dir="${VEYRA_INSTALL_DIR:-}"
if [ -z "$dir" ]; then
  for d in /usr/local/bin /opt/homebrew/bin "$HOME/.local/bin" "$HOME/bin"; do
    if on_path "$d" && [ -d "$d" ] && [ -w "$d" ]; then dir="$d"; break; fi
  done
fi
[ -n "$dir" ] || dir="$HOME/.local/bin"
mkdir -p "$dir"
install -m 0755 "$src" "$dir/veyra"
# A browser-downloaded archive marks its files "quarantined", which makes macOS warn on first run.
if [ "$(uname -s)" = Darwin ]; then xattr -d com.apple.quarantine "$dir/veyra" 2>/dev/null || true; fi

echo "Installed veyra to $dir/veyra"
if on_path "$dir"; then
  echo "Run:  veyra"
else
  # The folder is not on the PATH: add it to the shell's startup file. A script cannot change
  # the shell that launched it, so the current terminal needs the one-line export below.
  case "$(basename "${SHELL:-sh}")" in
    zsh) rc="${ZDOTDIR:-$HOME}/.zshrc"; line="export PATH=\"$dir:\$PATH\"" ;;
    bash) if [ "$(uname -s)" = Darwin ]; then rc="$HOME/.bash_profile"; else rc="$HOME/.bashrc"; fi
          line="export PATH=\"$dir:\$PATH\"" ;;
    fish) rc="$HOME/.config/fish/config.fish"; line="fish_add_path \"$dir\"" ;;
    *) rc="$HOME/.profile"; line="export PATH=\"$dir:\$PATH\"" ;;
  esac
  if [ -f "$rc" ] && grep -qF "$dir" "$rc" 2>/dev/null; then
    echo "$dir is already mentioned in $rc."
  else
    mkdir -p "$(dirname "$rc")"
    printf '\n# added by the veyra installer\n%s\n' "$line" >> "$rc"
    echo "Added $dir to your PATH in $rc."
  fi
  echo "To use it now:  export PATH=\"$dir:\$PATH\"   (new terminals pick it up on their own)"
  echo "Then run:  veyra"
fi
if ! command -v ffmpeg >/dev/null 2>&1; then
  echo "Note: scanning videos needs FFmpeg (macOS: brew install ffmpeg; Linux: sudo apt install ffmpeg)."
fi
