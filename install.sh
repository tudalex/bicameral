#!/bin/sh
# Install bicameral from its GitHub releases.
#
#   curl -fsSL https://raw.githubusercontent.com/tudalex/bicameral/master/install.sh | sh
#
# Environment:
#   BICAMERAL_VERSION      release tag to install, e.g. v0.1.0 (default: latest)
#   BICAMERAL_INSTALL_DIR  where to put the binary (default: ~/.local/bin)
set -eu

repo=tudalex/bicameral
version=${BICAMERAL_VERSION:-latest}
install_dir=${BICAMERAL_INSTALL_DIR:-$HOME/.local/bin}

if [ "$version" = latest ]; then
	base=https://github.com/$repo/releases/latest/download
else
	base=https://github.com/$repo/releases/download/$version
fi
base=${BICAMERAL_DOWNLOAD_URL:-$base} # for mirrors and testing

fail() {
	echo "bicameral install: $*" >&2
	exit 1
}

case $(uname -s) in
Darwin) os=darwin ;;
Linux) os=linux ;;
*) fail "unsupported OS: $(uname -s) (macOS and Linux only)" ;;
esac

case $(uname -m) in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*) fail "unsupported architecture: $(uname -m) (amd64 and arm64 only)" ;;
esac

# A Rosetta-translated shell on Apple silicon reports x86_64; prefer native.
if [ "$os" = darwin ] && [ "$arch" = amd64 ] &&
	[ "$(sysctl -n sysctl.proc_translated 2>/dev/null)" = 1 ]; then
	arch=arm64
fi

if command -v sha256sum >/dev/null 2>&1; then
	sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
	sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
	fail "need sha256sum or shasum to verify the download"
fi

name=bicameral_${os}_${arch}
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "Downloading $name ($version)..."
curl -fsSL -o "$tmp/$name.tar.gz" "$base/$name.tar.gz" ||
	fail "download failed: $base/$name.tar.gz"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt" ||
	fail "download failed: $base/checksums.txt"

want=$(awk -v f="$name.tar.gz" '$2 == f { print $1 }' "$tmp/checksums.txt")
[ -n "$want" ] || fail "$name.tar.gz is not listed in checksums.txt"
[ "$(sha256 "$tmp/$name.tar.gz")" = "$want" ] || fail "checksum mismatch for $name.tar.gz"

tar -xzf "$tmp/$name.tar.gz" -C "$tmp"
mkdir -p "$install_dir"
# Install via rename so a running bicameral keeps its old binary.
cp "$tmp/$name/bicameral" "$install_dir/.bicameral.new"
chmod 755 "$install_dir/.bicameral.new"
mv -f "$install_dir/.bicameral.new" "$install_dir/bicameral"

echo "Installed bicameral to $install_dir/bicameral"
case ":$PATH:" in
*":$install_dir:"*) ;;
*) echo "Note: $install_dir is not on your PATH. Add it, e.g.:
  echo 'export PATH=\"$install_dir:\$PATH\"' >> ~/.profile" ;;
esac
