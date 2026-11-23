#!/bin/sh
# uirefs uninstaller: stop and remove the user service, remove the MCP entries
# the installer added and delete the installed binary. The data directory is
# kept unless --purge-data is given.
#
#   ./uninstall.sh [--purge-data] [--yes] [--dry-run] [-h]
#
# Environment:
#   UIREFS_BIN         directory of the binary      (default ~/.local/bin)
#   UIREFS_DATA        data directory               (default ~/.local/share/uirefs)
#   UIREFS_UNIT        systemd unit / launchd label (default uirefs)
#   HERMES_PROFILE     hermes profile to remove the MCP server from
set -eu

MCP_NAME=uirefs
ALL_CLIENTS="claude codex hermes pi"

# ---------- helpers ----------

say() { printf '%s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }
die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

show_cmd() {
	_s=
	for _a in "$@"; do
		case $_a in
		'') _a="''" ;;
		*[!A-Za-z0-9_@%+=:,./-]*) _a="'$_a'" ;;
		esac
		_s="${_s:+$_s }$_a"
	done
	printf '%s' "$_s"
}

run() {
	if [ "$DRY_RUN" -eq 1 ]; then
		printf '+ %s\n' "$(show_cmd "$@")"
		return 0
	fi
	"$@"
}

ask_yn() {
	_q=$1
	_def=$2
	if [ "$ASSUME_YES" -eq 1 ] || [ "$DRY_RUN" -eq 1 ]; then
		return 0
	fi
	if [ ! -t 0 ]; then
		# No answers on stdin: take the default instead of blocking.
		printf '%s (no TTY: %s)\n' "$_q" "$_def"
		if [ "$_def" = yes ]; then return 0; fi
		return 1
	fi
	if [ "$_def" = yes ]; then _hint='[Y/n]'; else _hint='[y/N]'; fi
	printf '%s %s ' "$_q" "$_hint"
	_ans=
	IFS= read -r _ans || _ans=
	case $_ans in
	[Yy] | [Yy][Ee][Ss]) return 0 ;;
	'')
		if [ "$_def" = yes ]; then return 0; fi
		return 1
		;;
	*) return 1 ;;
	esac
}

systemd_user_ok() {
	command -v systemctl >/dev/null 2>&1 || return 1
	systemctl --user show-environment >/dev/null 2>&1
}

hermes_cmd() {
	if [ -n "$HERMES_PROFILE" ]; then
		hermes -p "$HERMES_PROFILE" "$@"
	else
		hermes "$@"
	fi
}

absolute() {
	case $1 in
	/*) printf '%s' "$1" ;;
	*) printf '%s/%s' "$(pwd)" "$1" | sed 's|/\./|/|g' ;;
	esac
}

client_show() {
	case $1 in
	claude) _o=$(claude mcp get "$MCP_NAME" 2>/dev/null) || _o= ;;
	codex) _o=$(codex mcp get "$MCP_NAME" 2>/dev/null) || _o= ;;
	hermes) _o=$(hermes_cmd mcp list 2>/dev/null | grep -i "$MCP_NAME") || _o= ;;
	pi) _o=$(pi mcp list 2>/dev/null | grep -A1 -i "^$MCP_NAME:") || _o= ;;
	esac
	printf '%s' "$_o"
}

client_list_line() {
	case $1 in
	claude) _o=$(claude mcp list 2>/dev/null | grep -i "$MCP_NAME") || _o= ;;
	codex) _o=$(codex mcp list 2>/dev/null | grep -i "$MCP_NAME") || _o= ;;
	hermes) _o=$(hermes_cmd mcp list 2>/dev/null | grep -i "$MCP_NAME") || _o= ;;
	pi) _o=$(pi mcp list 2>/dev/null | grep -A1 -i "^$MCP_NAME:") || _o= ;;
	esac
	printf '%s' "$_o"
}

usage() {
	cat <<'EOF'
usage: ./uninstall.sh [options]

  --purge-data    also delete the data directory (asks first)
  --data DIR      data directory (default ~/.local/share/uirefs, env UIREFS_DATA)
  --bin DIR       directory of the binary (default ~/.local/bin, env UIREFS_BIN)
  --yes           answer yes to every question
  --dry-run       print every command and change nothing
  -h, --help      this help

Env: UIREFS_BIN UIREFS_DATA UIREFS_UNIT HERMES_PROFILE
EOF
}

# ---------- arguments ----------

PURGE=0
ASSUME_YES=0
DRY_RUN=0
OPT_DATA=
OPT_BIN=

while [ $# -gt 0 ]; do
	case $1 in
	--purge-data) PURGE=1 ;;
	--data) [ $# -ge 2 ] || die "--data needs a value"; OPT_DATA=$2; shift ;;
	--data=*) OPT_DATA=${1#--data=} ;;
	--bin) [ $# -ge 2 ] || die "--bin needs a value"; OPT_BIN=$2; shift ;;
	--bin=*) OPT_BIN=${1#--bin=} ;;
	--yes | -y) ASSUME_YES=1 ;;
	--dry-run) DRY_RUN=1 ;;
	-h | --help) usage; exit 0 ;;
	*) die "unknown argument: $1 (try --help)" ;;
	esac
	shift
done

DATA=${OPT_DATA:-${UIREFS_DATA:-$HOME/.local/share/uirefs}}
BIN_DIR=${OPT_BIN:-${UIREFS_BIN:-$HOME/.local/bin}}
# Relative paths would not name the same files the installer used.
DATA=$(absolute "$DATA")
BIN_DIR=$(absolute "$BIN_DIR")
UNIT=${UIREFS_UNIT:-uirefs}
HERMES_PROFILE=${HERMES_PROFILE:-}
USER=${USER:-$(id -un)}
unit_dir=${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user
unit_file=$unit_dir/$UNIT.service
plist=$HOME/Library/LaunchAgents/$UNIT.plist

say "uirefs uninstall"
say "  binary   $BIN_DIR/uirefs"
say "  data     $DATA"
case $(uname -s) in
Linux) say "  service  $unit_file" ;;
Darwin) say "  service  $plist" ;;
*) say "  service  $UNIT" ;;
esac

# ---------- service ----------

say ""
case $(uname -s) in
Linux)
	if systemd_user_ok; then
		if [ -f "$unit_file" ]; then
			say "service: stopping and removing $unit_file"
			run systemctl --user disable --now "$UNIT" || warn "could not stop $UNIT (it may not be running)"
			run rm -f "$unit_file"
			run systemctl --user daemon-reload
		else
			say "service: no unit at $unit_file"
		fi
	else
		say "service: systemd --user is not available here"
	fi
	;;
Darwin)
	if [ -f "$plist" ]; then
		say "service: unloading and removing $plist"
		run launchctl unload "$plist" || warn "could not unload $UNIT (it may not be running)"
		run rm -f "$plist"
	else
		say "service: no agent at $plist"
	fi
	;;
*)
	say "service: no supported service manager on $(uname -s)"
	;;
esac

# ---------- MCP entries ----------

say ""
for _c in $ALL_CLIENTS; do
	if ! command -v "$_c" >/dev/null 2>&1; then
		say "mcp: $_c not found on PATH, skipped"
		continue
	fi
	_existing=$(client_show "$_c")
	if [ -z "$_existing" ]; then
		say "mcp: $_c has no uirefs entry"
		continue
	fi
	if ! ask_yn "Remove the uirefs entry from $_c?" yes; then
		say "mcp: $_c left unchanged"
		continue
	fi
	case $_c in
	claude) run claude mcp remove --scope user "$MCP_NAME" ;;
	codex) run codex mcp remove "$MCP_NAME" ;;
	pi) run pi mcp remove "$MCP_NAME" ;;
	hermes) run hermes_cmd mcp remove "$MCP_NAME" ;;
	esac
	if [ "$DRY_RUN" -eq 0 ]; then
		if [ -n "$(client_list_line "$_c")" ]; then
			warn "$_c still lists a uirefs entry"
		else
			say "mcp: $_c -> no uirefs entry"
		fi
	fi
done

# ---------- binary ----------

say ""
if [ -f "$BIN_DIR/uirefs" ]; then
	say "binary: removing $BIN_DIR/uirefs"
	run rm -f "$BIN_DIR/uirefs"
else
	say "binary: $BIN_DIR/uirefs is not installed"
fi

# ---------- data ----------

say ""
if [ "$PURGE" -eq 0 ]; then
	say "data: kept at $DATA (--purge-data removes it)"
elif [ ! -d "$DATA" ]; then
	say "data: $DATA does not exist"
else
	_ok=1
	if [ "$ASSUME_YES" -eq 0 ] && [ "$DRY_RUN" -eq 0 ]; then
		say "data: $DATA holds the SQLite file and every asset; deleting it is permanent"
		if ! ask_yn "Delete $DATA?" no; then _ok=0; fi
	fi
	if [ "$_ok" -eq 1 ]; then
		run rm -rf "$DATA"
		say "data: removed $DATA"
	else
		say "data: kept at $DATA"
	fi
fi

say ""
say "uirefs is uninstalled."
