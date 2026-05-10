#!/usr/bin/env bash
set -euo pipefail

REPO="abronte/chronicle"
INSTALL_DIR="${CHRONICLE_INSTALL_DIR:-$HOME/.local/bin}"
BINARY_NAME="chronicle"

need() {
	if ! command -v "$1" >/dev/null 2>&1; then
		echo "Error: $1 is required to install chronicle." >&2
		exit 1
	fi
}

detect_asset() {
	local os arch ext=""

	case "$(uname -s)" in
		Linux) os="linux" ;;
		Darwin) os="darwin" ;;
		MINGW*|MSYS*|CYGWIN*) os="windows"; ext=".exe" ;;
		*)
			echo "Error: unsupported operating system: $(uname -s)" >&2
			exit 1
			;;
	esac

	case "$(uname -m)" in
		x86_64|amd64) arch="amd64" ;;
		arm64|aarch64) arch="arm64" ;;
		*)
			echo "Error: unsupported architecture: $(uname -m)" >&2
			exit 1
			;;
	esac

	printf '%s-%s-%s%s\n' "$BINARY_NAME" "$os" "$arch" "$ext"
}

latest_tag() {
	local api_url="https://api.github.com/repos/${REPO}/releases/latest"

	curl -fsSL "$api_url" \
		| sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' \
		| head -n 1
}

need curl
need install
need mktemp
need sed
need head
need uname

asset="$(detect_asset)"
tag="${CHRONICLE_VERSION:-$(latest_tag)}"

if [[ -z "$tag" ]]; then
	echo "Error: unable to determine latest chronicle release." >&2
	exit 1
fi

download_url="https://github.com/${REPO}/releases/download/${tag}/${asset}"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

echo "Installing chronicle ${tag} for ${asset#chronicle-}..."

curl -fL --progress-bar "$download_url" -o "$tmp_dir/$BINARY_NAME"

mkdir -p "$INSTALL_DIR"
install -m 0755 "$tmp_dir/$BINARY_NAME" "$INSTALL_DIR/$BINARY_NAME"

echo "Installed chronicle to $INSTALL_DIR/$BINARY_NAME"

if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
	echo "Note: $INSTALL_DIR is not in your PATH."
	echo "Add this to your shell config:"
	echo "  export PATH=\"$INSTALL_DIR:\$PATH\""
fi
