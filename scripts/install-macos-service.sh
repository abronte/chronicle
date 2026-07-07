#!/usr/bin/env bash
set -euo pipefail

LABEL="${CHRONICLE_SERVICE_LABEL:-com.abronte.chronicle}"
LOG_DIR="${CHRONICLE_LOG_DIR:-$HOME/Library/Logs/chronicle}"
PLIST_DIR="$HOME/Library/LaunchAgents"
START_SERVICE=1
BINARY_PATH="${CHRONICLE_BINARY:-}"
SERVICE_ARGS=(watch)
SERVICE_ARGS_SET=0

usage() {
	cat <<'EOF'
Usage: install-macos-service.sh [options] [-- chronicle-args...]

Install Chronicle as a macOS user LaunchAgent that starts at login.

Options:
  --binary PATH   Path to the chronicle binary.
                  Defaults to ~/.local/bin/chronicle, then PATH, then ./bin/chronicle.
  --label LABEL   launchd service label. Default: com.abronte.chronicle
  --no-start      Install the service without starting it now.
  -h, --help      Show this help.

Examples:
  scripts/install-macos-service.sh
  scripts/install-macos-service.sh -- --addr :12346
  scripts/install-macos-service.sh --binary /usr/local/bin/chronicle -- web
EOF
}

die() {
	echo "Error: $*" >&2
	exit 1
}

need() {
	if ! command -v "$1" >/dev/null 2>&1; then
		die "$1 is required."
	fi
}

absolute_path() {
	local path="$1"
	local dir base

	case "$path" in
		/*) ;;
		*) path="$PWD/$path" ;;
	esac

	dir="$(dirname "$path")"
	base="$(basename "$path")"
	dir="$(cd "$dir" && pwd -P)" || return 1
	printf '%s/%s\n' "$dir" "$base"
}

xml_escape() {
	local value="$1"
	value="${value//&/&amp;}"
	value="${value//</&lt;}"
	value="${value//>/&gt;}"
	printf '%s' "$value"
}

find_binary() {
	local default_binary="$HOME/.local/bin/chronicle"

	if [[ -n "$BINARY_PATH" ]]; then
		absolute_path "$BINARY_PATH"
		return
	fi

	if [[ -x "$default_binary" ]]; then
		printf '%s\n' "$default_binary"
		return
	fi

	if command -v chronicle >/dev/null 2>&1; then
		command -v chronicle
		return
	fi

	if [[ -x "$PWD/bin/chronicle" ]]; then
		absolute_path "$PWD/bin/chronicle"
		return
	fi

	return 1
}

write_plist() {
	local plist_path="$1"
	local stdout_path="$LOG_DIR/stdout.log"
	local stderr_path="$LOG_DIR/stderr.log"
	local arg

	mkdir -p "$PLIST_DIR" "$LOG_DIR"
	touch "$stdout_path" "$stderr_path"

	{
		printf '%s\n' '<?xml version="1.0" encoding="UTF-8"?>'
		printf '%s\n' '<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">'
		printf '%s\n' '<plist version="1.0">'
		printf '%s\n' '<dict>'
		printf '\t%s\n\t<string>%s</string>\n' '<key>Label</key>' "$(xml_escape "$LABEL")"
		printf '\t%s\n\t<array>\n' '<key>ProgramArguments</key>'
		printf '\t\t<string>%s</string>\n' "$(xml_escape "$BINARY_PATH")"
		for arg in "${SERVICE_ARGS[@]}"; do
			printf '\t\t<string>%s</string>\n' "$(xml_escape "$arg")"
		done
		printf '\t</array>\n'
		printf '\t%s\n\t<string>%s</string>\n' '<key>WorkingDirectory</key>' "$(xml_escape "$HOME")"
		printf '\t%s\n\t<string>Background</string>\n' '<key>ProcessType</key>'
		printf '\t%s\n\t<true/>\n' '<key>RunAtLoad</key>'
		printf '\t%s\n\t<true/>\n' '<key>KeepAlive</key>'
		printf '\t%s\n\t<string>%s</string>\n' '<key>StandardOutPath</key>' "$(xml_escape "$stdout_path")"
		printf '\t%s\n\t<string>%s</string>\n' '<key>StandardErrorPath</key>' "$(xml_escape "$stderr_path")"
		printf '%s\n' '</dict>'
		printf '%s\n' '</plist>'
	} >"$plist_path"
}

while (($#)); do
	case "$1" in
		--binary)
			[[ $# -ge 2 ]] || die "--binary requires a path."
			BINARY_PATH="$2"
			shift 2
			;;
		--label)
			[[ $# -ge 2 ]] || die "--label requires a launchd label."
			LABEL="$2"
			shift 2
			;;
		--no-start)
			START_SERVICE=0
			shift
			;;
		-h|--help)
			usage
			exit 0
			;;
		--)
			shift
			if (($#)); then
				SERVICE_ARGS=("$@")
				SERVICE_ARGS_SET=1
			fi
			break
			;;
		*)
			die "unknown option: $1"
			;;
	esac
done

[[ -n "$LABEL" && "$LABEL" != */* ]] || die "launchd label must be non-empty and must not contain '/'."

if [[ "$(uname -s)" != "Darwin" ]]; then
	die "macOS is required."
fi

need launchctl
need plutil

BINARY_PATH="$(find_binary)" || die "chronicle binary not found. Run scripts/install.sh first, or pass --binary PATH."
BINARY_PATH="$(absolute_path "$BINARY_PATH")"
[[ -x "$BINARY_PATH" ]] || die "$BINARY_PATH is not executable."

if [[ "$SERVICE_ARGS_SET" -eq 0 && -n "${CHRONICLE_SERVICE_ARGS:-}" ]]; then
	# shellcheck disable=SC2206
	SERVICE_ARGS=(${CHRONICLE_SERVICE_ARGS})
fi

PLIST_PATH="$PLIST_DIR/$LABEL.plist"
DOMAIN="gui/$(id -u)"

write_plist "$PLIST_PATH"
chmod 0644 "$PLIST_PATH"
plutil -lint "$PLIST_PATH" >/dev/null

launchctl bootout "$DOMAIN" "$PLIST_PATH" >/dev/null 2>&1 || true
launchctl bootstrap "$DOMAIN" "$PLIST_PATH"
launchctl enable "$DOMAIN/$LABEL"

if [[ "$START_SERVICE" -eq 1 ]]; then
	launchctl kickstart -k "$DOMAIN/$LABEL"
fi

echo "Installed Chronicle launchd service:"
echo "  Label:  $LABEL"
echo "  Plist:  $PLIST_PATH"
echo "  Binary: $BINARY_PATH"
echo "  Args:   ${SERVICE_ARGS[*]}"
echo "  Logs:   $LOG_DIR"
