#!/bin/sh
# xensus uninstaller — finds and removes the xensus binary. The Xensus data
# directory (SQLite database, audit log) is intentionally NOT touched —
# operators are expected to delete it themselves once they're sure they
# don't need the audit history. POSIX sh, no bash extensions.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/excelano/xensus/main/uninstall.sh | sh
#
# Environment variables:
#   XENSUS_UNINSTALL_YES=1   Skip the interactive confirmation (assume yes)

set -eu

BIN="xensus"

say() { printf '%s\n' "$*" >&2; }
err() { say "error: $*"; exit 1; }

read_yes() {
	prompt="$1"
	if [ "${XENSUS_UNINSTALL_YES:-0}" = "1" ]; then
		return 0
	fi
	if [ ! -t 0 ] && [ ! -e /dev/tty ]; then
		err "no terminal available for confirmation; re-run with XENSUS_UNINSTALL_YES=1 to skip the prompt"
	fi
	printf '%s [y/N]: ' "$prompt" >&2
	if [ -e /dev/tty ]; then
		read ans </dev/tty
	else
		read ans
	fi
	case "$ans" in
		y|Y|yes|YES) return 0 ;;
		*) return 1 ;;
	esac
}

if ! command -v "$BIN" >/dev/null 2>&1; then
	say "$BIN is not on PATH; nothing to uninstall."
	say "If you installed to a custom location, remove it manually:"
	say "    rm /path/to/$BIN"
	exit 0
fi

TARGET=$(command -v "$BIN")
say "Found $BIN at $TARGET"

if [ ! -w "$TARGET" ] && [ ! -w "$(dirname "$TARGET")" ]; then
	err "$TARGET is not writable; re-run with sudo to remove it"
fi

if ! read_yes "Remove $TARGET?"; then
	say "Aborted."
	exit 1
fi

rm -f "$TARGET" || err "could not remove $TARGET"
say "Removed $TARGET"

hash -r 2>/dev/null || true

LEFTOVER=$(command -v "$BIN" 2>/dev/null || true)
if [ -n "$LEFTOVER" ]; then
	say ""
	say "Note: another $BIN binary is still on PATH at $LEFTOVER"
	say "Re-run this uninstaller to remove it, or remove it manually."
fi

say ""
say "Data directory NOT removed."
say "Xensus stores its SQLite database, tenant binding, and audit log under"
say "the directory you set as XENSUS_DATA_DIR. The uninstaller leaves that"
say "alone on purpose — deleting an audit log is a deliberate act. If you"
say "want to remove the data, do it explicitly:"
say "    rm -rf /path/to/your/XENSUS_DATA_DIR"
say ""
say "Done."
