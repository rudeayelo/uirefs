#!/bin/sh
# uirefs installer: build the static binary, install it, run it as a user
# service and register the MCP server in the clients found on PATH.
#
# POSIX sh on purpose: the public form of this file is `curl … | sh`, so it
# also runs outside a checkout, clones the repo and re-runs itself from there.
#
#   ./install.sh [--port N] [--data DIR] [--no-service] [--no-mcp]
#                [--mcp claude,codex,hermes,pi] [--yes] [--force]
#                [--dry-run] [-h]
#
# Environment:
#   UIREFS_BIN         directory for the binary      (default ~/.local/bin)
#   UIREFS_DATA        data directory                (default ~/.local/share/uirefs)
#   UIREFS_PORT        port on 127.0.0.1             (default 4880)
#   UIREFS_UNIT        systemd unit / launchd label  (default uirefs)
#   UIREFS_PUBLIC_URL  URL agents should use, e.g. a private HTTPS name
#   UIREFS_EMBED_URL   optional embeddings endpoint, passed to the service
#   UIREFS_DIR         clone target for the `curl | sh` form (default ~/uirefs)
#   UIREFS_REPO        git remote for that clone
#   HERMES_PROFILE     hermes profile to register the MCP server in
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

# show_cmd quotes arguments so the printed command can be pasted back.
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

# run executes a command, or only prints it with --dry-run.
run() {
	if [ "$DRY_RUN" -eq 1 ]; then
		printf '+ %s\n' "$(show_cmd "$@")"
		return 0
	fi
	"$@"
}

run_in() {
	_d=$1
	shift
	(cd "$_d" && run "$@")
}

# write_file PATH reads stdin; it only touches the file when the content changed.
write_file() {
	if [ "$DRY_RUN" -eq 1 ]; then
		printf '+ write %s\n' "$1"
		cat >/dev/null
		return 0
	fi
	_f=$1.tmp.$$
	cat >"$_f"
	if [ -f "$1" ] && cmp -s "$_f" "$1"; then
		say "unchanged: $1"
		rm -f "$_f"
		return 0
	fi
	mv "$_f" "$1"
	say "wrote: $1"
}

# ask_yn prints a question and reads one line: 0 means yes.
ask_yn() {
	_q=$1
	_def=$2
	if [ "$ASSUME_YES" -eq 1 ] || [ "$DRY_RUN" -eq 1 ]; then
		return 0
	fi
	if [ ! -t 0 ]; then
		# Piped input (the `curl | sh` form) is the script, not answers.
		printf '%s (no TTY: no, pass --yes to accept)\n' "$_q"
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

# sd_escape escapes a sed replacement and doubles % for systemd specifiers.
sd_escape() {
	printf '%s' "$1" | sed -e 's/%/%%/g' -e 's/[\\&|]/\\&/g'
}

xml_escape() {
	printf '%s' "$1" | sed -e 's/&/\&amp;/g' -e 's/</\&lt;/g' -e 's/>/\&gt;/g'
}

absolute() {
	case $1 in
	/*) printf '%s' "$1" ;;
	*) printf '%s/%s' "$(pwd)" "$1" | sed 's|/\./|/|g' ;;
	esac
}

http_code() {
	curl -s -o /dev/null -m 2 -w '%{http_code}' "$1" 2>/dev/null || true
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

# client_show prints what a client says about the uirefs entry; empty if none.
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

# client_remove deletes the entry; needed before re-adding with another URL.
client_remove() {
	case $1 in
	claude) run claude mcp remove --scope user "$MCP_NAME" ;;
	codex) run codex mcp remove "$MCP_NAME" ;;
	pi) run pi mcp remove "$MCP_NAME" ;;
	hermes) run hermes_cmd mcp remove "$MCP_NAME" ;;
	esac
}

usage() {
	cat <<'EOF'
usage: ./install.sh [options]

  --port N        port on 127.0.0.1 (default 4880, env UIREFS_PORT)
  --data DIR      data directory (default ~/.local/share/uirefs, env UIREFS_DATA)
  --no-service    do not install or start the user service
  --no-mcp        do not register the MCP server in any client
  --mcp LIST      only these clients: claude,codex,hermes,pi
  --yes           answer yes to every question
  --force         replace an existing uirefs MCP entry that has another URL
  --dry-run       print every command and change nothing
  -h, --help      this help

Env: UIREFS_BIN UIREFS_DATA UIREFS_PORT UIREFS_UNIT UIREFS_PUBLIC_URL
     UIREFS_EMBED_URL UIREFS_DIR UIREFS_REPO HERMES_PROFILE
EOF
}

# ---------- early flags and bootstrap ----------

# The bootstrap below re-runs this script with the original arguments, so they
# are parsed only after it; --dry-run and --help are read from a copy.
DRY_RUN=0
for _a in "$@"; do
	case $_a in
	--dry-run) DRY_RUN=1 ;;
	-h | --help)
		usage
		exit 0
		;;
	esac
done

REPO=$(
	case $0 in
	*/*) dirname "$0" ;;
	*) printf '.' ;;
	esac
)
REPO=$(cd "$REPO" 2>/dev/null && pwd) || REPO=
if [ -z "$REPO" ] || ! grep -qs '^module github.com/uesteibar/uirefs$' "$REPO/go.mod"; then
	REPO=
	TARGET=${UIREFS_DIR:-$HOME/uirefs}
	REMOTE=${UIREFS_REPO:-https://github.com/uesteibar/uirefs.git}
	say "not inside a uirefs checkout: using $TARGET"
	if [ "$DRY_RUN" -eq 1 ]; then
		if [ ! -d "$TARGET/.git" ]; then
			printf '+ %s\n' "$(show_cmd git clone "$REMOTE" "$TARGET")"
		fi
		printf '+ %s\n' "$(show_cmd sh "$TARGET/install.sh" "$@")"
		exit 0
	fi
	if [ "${UIREFS_BOOTSTRAPPED:-0}" = 1 ]; then
		die "clone at $TARGET still has no install.sh"
	fi
	if [ -d "$TARGET/.git" ]; then
		say "using the existing checkout at $TARGET"
	elif [ -e "$TARGET" ]; then
		die "$TARGET exists and is not a git checkout; set UIREFS_DIR to another path"
	else
		command -v git >/dev/null 2>&1 || die "git is required to clone the repo"
		run git clone "$REMOTE" "$TARGET"
	fi
	UIREFS_BOOTSTRAPPED=1
	export UIREFS_BOOTSTRAPPED
	exec sh "$TARGET/install.sh" "$@"
fi

# ---------- arguments ----------

OPT_PORT=
OPT_DATA=
DO_SERVICE=1
DO_MCP=1
MCP_LIST=
ASSUME_YES=0
FORCE=0

while [ $# -gt 0 ]; do
	case $1 in
	--port) [ $# -ge 2 ] || die "--port needs a value"; OPT_PORT=$2; shift ;;
	--port=*) OPT_PORT=${1#--port=} ;;
	--data) [ $# -ge 2 ] || die "--data needs a value"; OPT_DATA=$2; shift ;;
	--data=*) OPT_DATA=${1#--data=} ;;
	--mcp) [ $# -ge 2 ] || die "--mcp needs a value"; MCP_LIST=$2; shift ;;
	--mcp=*) MCP_LIST=${1#--mcp=} ;;
	--no-service) DO_SERVICE=0 ;;
	--no-mcp) DO_MCP=0 ;;
	--yes | -y) ASSUME_YES=1 ;;
	--force) FORCE=1 ;;
	--dry-run) DRY_RUN=1 ;;
	-h | --help) usage; exit 0 ;;
	*) die "unknown argument: $1 (try --help)" ;;
	esac
	shift
done

PORT=${OPT_PORT:-${UIREFS_PORT:-4880}}
DATA=${OPT_DATA:-${UIREFS_DATA:-$HOME/.local/share/uirefs}}
BIN_DIR=${UIREFS_BIN:-$HOME/.local/bin}
# The service starts with its own working directory: relative paths must not
# leak into the unit.
DATA=$(absolute "$DATA")
BIN_DIR=$(absolute "$BIN_DIR")
UNIT=${UIREFS_UNIT:-uirefs}
HERMES_PROFILE=${HERMES_PROFILE:-}
USER=${USER:-$(id -un)}

case $PORT in
'' | *[!0-9]*) die "port must be a number, got: $PORT" ;;
esac
[ "$PORT" -ge 1 ] && [ "$PORT" -le 65535 ] || die "port must be between 1 and 65535, got: $PORT"

if [ -n "$MCP_LIST" ]; then
	CLIENTS=$(printf '%s' "$MCP_LIST" | tr ',' ' ')
	for _c in $CLIENTS; do
		case " $ALL_CLIENTS " in
		*" $_c "*) ;;
		*) die "unknown client in --mcp: $_c (choose from: $ALL_CLIENTS)" ;;
		esac
	done
else
	CLIENTS=$ALL_CLIENTS
fi

LOCAL_BASE="http://127.0.0.1:$PORT"
LOCAL_MCP="$LOCAL_BASE/mcp"
if [ -n "${UIREFS_PUBLIC_URL:-}" ]; then
	PUBLIC_BASE=${UIREFS_PUBLIC_URL%/}
	WEB_URL=$PUBLIC_BASE
	MCP_URL=$PUBLIC_BASE/mcp
else
	WEB_URL=$LOCAL_BASE/
	MCP_URL=$LOCAL_MCP
fi

# ---------- what it will do ----------

# (the `curl … | sh` bootstrap, when there is no checkout, ran above)

say "uirefs install"
say "  repo     $REPO"
say "  binary   $BIN_DIR/uirefs"
say "  data     $DATA"
say "  port     $PORT"
if [ "$DO_SERVICE" -eq 1 ]; then
	say "  service  $UNIT"
else
	say "  service  none (--no-service)"
fi
if [ "$DO_MCP" -eq 1 ]; then
	say "  mcp      $MCP_URL"
else
	say "  mcp      none (--no-mcp)"
fi

# ---------- 1. preflight ----------

for _tool in git curl; do
	command -v "$_tool" >/dev/null 2>&1 || die "$_tool is required and was not found on PATH"
done

NEED_GO=$(sed -n 's/^go  *\([0-9][0-9.]*\).*/\1/p' "$REPO/go.mod" | head -1)
NEED_GO=${NEED_GO:-1.22}
GO=go
if ! command -v go >/dev/null 2>&1; then
	if command -v mise >/dev/null 2>&1 && [ -f "$REPO/mise.toml" ]; then
		say "go not found: installing the toolchain declared in mise.toml"
		run_in "$REPO" mise install
		if [ "$DRY_RUN" -eq 0 ]; then
			if command -v go >/dev/null 2>&1; then
				GO=go
			else
				_go=$(cd "$REPO" && mise which go 2>/dev/null) || _go=
				if [ -n "$_go" ]; then GO=$_go; fi
			fi
		fi
	fi
fi
if [ "$DRY_RUN" -eq 0 ]; then
	if ! _v=$("$GO" version 2>/dev/null); then
		die "go $NEED_GO or newer is required.
  install it with:  brew install go          (macOS)
                    apt install golang-go    (Debian/Ubuntu, needs sudo)
                    https://go.dev/dl/       (any platform)
  then run ./install.sh again"
	fi
	_v=$(printf '%s' "$_v" | sed -n 's/.*go\([0-9][0-9.]*\).*/\1/p')
	if ! awk -v need="$NEED_GO" -v have="$_v" 'BEGIN {
		n = split(need, a, "."); split(have, b, ".")
		for (i = 1; i <= n; i++) {
			if ((b[i] + 0) > (a[i] + 0)) exit 0
			if ((b[i] + 0) < (a[i] + 0)) exit 1
		}
		exit 0
	}' /dev/null; then
		die "go $NEED_GO or newer is required, found go$_v; update it (mise install) and re-run"
	fi
	say "go       go$_v (>= $NEED_GO)"
fi

# ---------- 2. build and install the binary ----------

say "building"
run_in "$REPO" env CGO_ENABLED=0 "$GO" build "-ldflags=-s -w" -o uirefs ./cmd/uirefs
run mkdir -p "$BIN_DIR"
# copy then rename: the running service keeps its old inode, so this works
# while uirefs is running.
run cp "$REPO/uirefs" "$BIN_DIR/uirefs.new"
run chmod 755 "$BIN_DIR/uirefs.new"
run mv -f "$BIN_DIR/uirefs.new" "$BIN_DIR/uirefs"
if [ "$DRY_RUN" -eq 0 ]; then
	_bytes=$(wc -c <"$BIN_DIR/uirefs" | tr -d ' ')
	_size=$(awk -v b="$_bytes" 'BEGIN { if (b >= 1048576) printf "%.1f MB", b / 1048576; else printf "%.0f KB", b / 1024 }' /dev/null)
	say "binary   $BIN_DIR/uirefs ($_size)"
	"$BIN_DIR/uirefs" version
	case ":$PATH:" in
	*":$BIN_DIR:"*) ;;
	*) warn "$BIN_DIR is not on PATH; add it to use \`uirefs\` directly" ;;
	esac
fi

# ---------- 3. service ----------

say ""
if [ "$DO_SERVICE" -eq 0 ]; then
	say "service: skipped (--no-service)"
	say "  run it by hand: $BIN_DIR/uirefs serve --port $PORT --data $DATA"
else
	run mkdir -p "$DATA"
	unit_dir=${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user
	plist=$HOME/Library/LaunchAgents/$UNIT.plist
	case $(uname -s) in
	Linux)
		if systemd_user_ok; then
			say "service: systemd --user unit $UNIT"
			run mkdir -p "$unit_dir"
			_embed=
			if [ -n "${UIREFS_EMBED_URL:-}" ]; then
				_embed="Environment=UIREFS_EMBED_URL=$(sd_escape "$UIREFS_EMBED_URL")"
			fi
			if [ -n "${UIREFS_PUBLIC_URL:-}" ]; then
				# The service answers only to this host name besides localhost and IPs.
				_embed="${_embed:+$_embed\\n}Environment=UIREFS_PUBLIC_URL=$(sd_escape "$UIREFS_PUBLIC_URL")"
			fi
			sed -e "s|@BIN@|$(sd_escape "$BIN_DIR/uirefs")|g" \
				-e "s|@DATA@|$(sd_escape "$DATA")|g" \
				-e "s|@PORT@|$PORT|g" \
				-e "s|@EMBED_ENV@|$_embed|g" "$REPO/deploy/uirefs.service" |
				write_file "$unit_dir/$UNIT.service"
			run systemctl --user daemon-reload
			run systemctl --user enable "$UNIT"
			run systemctl --user restart "$UNIT"
			_linger=$(loginctl show-user "$USER" -p Linger 2>/dev/null) || _linger=
			case $_linger in
			*Linger=yes*) ;;
			*)
				if command -v loginctl >/dev/null 2>&1; then
					say "service: enabling linger so $UNIT runs without a login session"
					run loginctl enable-linger "$USER" || warn "could not enable linger; the service stops when you log out"
				fi
				;;
			esac
		else
			say "service: systemd --user is not available on this machine"
			say "  run it by hand: $BIN_DIR/uirefs serve --port $PORT --data $DATA"
		fi
		;;
	Darwin)
		say "service: launchd agent $UNIT"
		_env_xml=
		_env_items=
		if [ -n "${UIREFS_EMBED_URL:-}" ]; then
			_env_items="<key>UIREFS_EMBED_URL</key><string>$(xml_escape "$UIREFS_EMBED_URL")</string>"
		fi
		if [ -n "${UIREFS_PUBLIC_URL:-}" ]; then
			_env_items="$_env_items<key>UIREFS_PUBLIC_URL</key><string>$(xml_escape "$UIREFS_PUBLIC_URL")</string>"
		fi
		if [ -n "$_env_items" ]; then
			_env_xml="\t<key>EnvironmentVariables</key><dict>$_env_items</dict>"
		fi
		plist_content() {
			cat <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>$(xml_escape "$UNIT")</string>
	<key>ProgramArguments</key>
	<array>
		<string>$(xml_escape "$BIN_DIR/uirefs")</string>
		<string>serve</string>
		<string>--port</string>
		<string>$PORT</string>
		<string>--data</string>
		<string>$(xml_escape "$DATA")</string>
	</array>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
	<key>StandardOutPath</key><string>$(xml_escape "$DATA/uirefs.log")</string>
	<key>StandardErrorPath</key><string>$(xml_escape "$DATA/uirefs.log")</string>
$_env_xml
</dict>
</plist>
EOF
		}
		run mkdir -p "$(dirname "$plist")"
		plist_content | write_file "$plist"
		run launchctl unload "$plist" || true
		if ! run launchctl load -w "$plist"; then
			warn "could not load the launchd agent; load it by hand: launchctl load -w $plist"
		fi
		say "  launchd is untested in this repo; check it with: launchctl list | grep $UNIT"
		;;
	*)
		say "service: no supported service manager on $(uname -s)"
		say "  run it by hand: $BIN_DIR/uirefs serve --port $PORT --data $DATA"
		;;
	esac
fi

# ---------- 4. health ----------

HEALTH_OK=0
say ""
if [ "$DRY_RUN" -eq 1 ]; then
	say "health: would wait up to 15 s for $LOCAL_BASE/api/references"
	say "health: would run $BIN_DIR/uirefs doctor --data $DATA"
	HEALTH_OK=1
else
	_i=0
	while [ "$_i" -lt 15 ]; do
		if [ "$(http_code "$LOCAL_BASE/api/references")" = 200 ]; then
			HEALTH_OK=1
			break
		fi
		_i=$((_i + 1))
		sleep 1
	done
	if [ "$HEALTH_OK" -eq 1 ]; then
		say "health: $LOCAL_BASE/api/references -> 200"
		run "$BIN_DIR/uirefs" doctor --data "$DATA"
	elif [ "$DO_SERVICE" -eq 1 ]; then
		warn "no answer on $LOCAL_BASE after 15 s"
		case $(uname -s) in
		Linux) say "  see: journalctl --user -u $UNIT -n 30 --no-pager" ;;
		Darwin) say "  see: $DATA/uirefs.log" ;;
		esac
		die "the service did not come up"
	else
		say "health: nothing answers on $LOCAL_BASE and no service was installed"
		say "  start it with: $BIN_DIR/uirefs serve --port $PORT --data $DATA"
	fi
fi

say ""
say "web      $WEB_URL"
say "mcp      $MCP_URL"
if [ -n "${UIREFS_PUBLIC_URL:-}" ]; then
	say "local    $LOCAL_MCP"
fi

# ---------- 5. MCP registration ----------

say ""
if [ "$DO_MCP" -eq 0 ]; then
	say "mcp: registration skipped (--no-mcp)"
else
	for _c in $CLIENTS; do
		if ! command -v "$_c" >/dev/null 2>&1; then
			say "mcp: $_c not found on PATH, skipped"
			continue
		fi
		_existing=$(client_show "$_c")
		if [ -n "$_existing" ]; then
			if printf '%s\n' "$_existing" | grep -qF "$MCP_URL"; then
				say "mcp: $_c already has uirefs at $MCP_URL"
				continue
			fi
			warn "$_c already has a uirefs entry with another URL:"
			printf '%s\n' "$_existing" | sed 's/^/         /' >&2
			say "         this install uses: $MCP_URL"
			if [ "$ASSUME_YES" -eq 1 ] && [ "$FORCE" -eq 0 ]; then
				die "$_c: refusing to replace that entry; re-run with --force to replace it"
			fi
			if ! ask_yn "Replace the uirefs entry in $_c?" no; then
				say "mcp: $_c left unchanged"
				continue
			fi
			client_remove "$_c"
		elif ! ask_yn "Register uirefs in $_c?" yes; then
			say "mcp: $_c skipped"
			continue
		fi

		case $_c in
		claude) run claude mcp add --scope user --transport http "$MCP_NAME" "$MCP_URL" ;;
		codex) run codex mcp add "$MCP_NAME" --url "$MCP_URL" ;;
		pi) run pi mcp add "$MCP_NAME" --url "$MCP_URL" ;;
		hermes)
			# hermes asks two questions: authentication (n) and then either
			# "enable all tools?" (Y) or "save anyway?" (Y).
			if [ "$DRY_RUN" -eq 1 ]; then
				printf '+ %s\n' "printf 'n\\nY\\n' | $(show_cmd hermes mcp add "$MCP_NAME" --url "$MCP_URL")"
			else
				if ! printf 'n\nY\n' | hermes_cmd mcp add "$MCP_NAME" --url "$MCP_URL"; then
					warn "hermes mcp add failed; run it by hand and answer its questions:"
					if [ -n "$HERMES_PROFILE" ]; then
						say "  hermes -p $HERMES_PROFILE mcp add $MCP_NAME --url $MCP_URL"
					else
						say "  hermes mcp add $MCP_NAME --url $MCP_URL"
					fi
					say "  authentication: n"
					say "  enable all tools: Y"
				fi
			fi
			;;
		esac

		if [ "$DRY_RUN" -eq 0 ]; then
			_line=$(client_list_line "$_c")
			if [ -n "$_line" ]; then
				say "mcp: $_c -> $(printf '%s' "$_line" | tr '\n' ' ')"
			else
				warn "$_c does not list a uirefs entry after adding it"
			fi
		fi
	done
fi

say ""
say "any other MCP client:"
say "  {\"mcpServers\": {\"uirefs\": {\"type\": \"http\", \"url\": \"$MCP_URL\"}}}"

# ---------- 6. smoke test ----------

say ""
if [ "$HEALTH_OK" -eq 0 ]; then
	say "smoke: skipped (nothing running on port $PORT)"
elif [ "$DRY_RUN" -eq 1 ]; then
	say "smoke: would POST initialize, tools/list and guide to $LOCAL_MCP"
else
	mcp_rpc() {
		printf '{"jsonrpc":"2.0","id":1,"method":"%s","params":%s}\n' "$1" "$2" |
			curl -fsS -m 10 -X POST \
				-H 'Content-Type: application/json' \
				-H 'Accept: application/json, text/event-stream' \
				--data-binary @- "$LOCAL_MCP"
	}
	_init=$(mcp_rpc initialize '{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"installer","version":"1"}}') ||
		die "smoke test: initialize failed against $LOCAL_MCP"
	case $_init in
	*'"serverInfo"'*) say "OK initialize (uirefs server)" ;;
	*) die "smoke test: unexpected initialize response: $_init" ;;
	esac

	_tools=$(mcp_rpc tools/list '{}') || die "smoke test: tools/list failed"
	_n=$(printf '%s' "$_tools" | grep -o '"inputSchema"' | wc -l | tr -d ' ')
	[ "$_n" = 7 ] || die "smoke test: tools/list returned $_n tools, expected 7"
	say "OK tools/list ($_n tools)"

	_guide=$(mcp_rpc tools/call '{"name":"guide","arguments":{}}') || die "smoke test: guide failed"
	case $_guide in
	*'"isError":true'*) die "smoke test: guide returned an error: $_guide" ;;
	esac
	case $_guide in
	*'usage guide'*) say "OK guide" ;;
	*) die "smoke test: unexpected guide response: $(printf '%s' "$_guide" | cut -c1-200)" ;;
	esac
fi

# ---------- 7. done ----------

say ""
if [ "$DO_SERVICE" -eq 1 ]; then
	say "after a rebuild: $BIN_DIR/uirefs is used by $UNIT; restart it with: systemctl --user restart $UNIT"
fi
if [ -n "${UIREFS_PUBLIC_URL:-}" ]; then
	say "uirefs has no auth: it is reachable at $PUBLIC_BASE only because you expose it; keep it on a private network."
else
	say "uirefs has no auth: keep it on 127.0.0.1 or a private network. Exposing it is your step."
fi
say ""
say "Next: call the \`guide\` tool of the uirefs MCP before saving or searching references."
