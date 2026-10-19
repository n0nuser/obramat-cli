#!/bin/sh
# Install the latest obramat release into ~/.local/bin, $PREFIX/bin or
# $OBRAMAT_INSTALL_DIR. Pin a version with VERSION=v0.1.0.
set -eu

repo="n0nuser/obramat-cli"
bin="obramat"

if [ -n "${OBRAMAT_INSTALL_DIR:-}" ]; then
	dir="$OBRAMAT_INSTALL_DIR"
elif [ -n "${PREFIX:-}" ]; then
	dir="$PREFIX/bin"
else
	dir="$HOME/.local/bin"
fi

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
linux | darwin) ;;
*)
	echo "$bin: unsupported OS '$os'. Download a release from https://github.com/$repo/releases" >&2
	exit 1
	;;
esac

arch=$(uname -m)
case "$arch" in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*)
	echo "$bin: unsupported architecture '$arch'. Download a release from https://github.com/$repo/releases" >&2
	exit 1
	;;
esac

command -v curl >/dev/null 2>&1 || {
	echo "$bin: curl is required" >&2
	exit 1
}

if [ -n "${VERSION:-}" ]; then
	tag=$VERSION
	case "$tag" in v*) ;; *) tag="v$tag" ;; esac
else
	tag=$(curl -fsSL "https://api.github.com/repos/$repo/releases/latest" |
		sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)
	[ -n "$tag" ] || {
		echo "$bin: could not find the latest release on GitHub" >&2
		exit 1
	}
fi
version=${tag#v}
asset="${bin}_${version}_${os}_${arch}.tar.gz"
base="https://github.com/$repo/releases/download/$tag"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

echo "downloading $bin $version ($os/$arch)"
curl -fsSL "$base/$asset" -o "$tmp/$asset"
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"

# macOS ships shasum, most Linux distributions ship sha256sum.
if command -v sha256sum >/dev/null 2>&1; then
	checksum="sha256sum"
else
	checksum="shasum -a 256"
fi
# The checksums file names every archive; verify this one (the download has
# already happened, and nothing is unpacked before it checks out).
(cd "$tmp" && grep " $asset\$" checksums.txt | $checksum -c - >/dev/null) || {
	echo "$bin: checksum verification failed for $asset; not installing" >&2
	exit 1
}

tar -xzf "$tmp/$asset" -C "$tmp" "$bin"
mkdir -p "$dir"
install -m 0755 "$tmp/$bin" "$dir/$bin"
echo "installed $bin $version to $dir/$bin"
case ":$PATH:" in
*":$dir:"*) ;;
*) echo "add $dir to your PATH (for example: export PATH=\"$dir:\$PATH\")" ;;
esac
