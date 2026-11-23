# Deploy

`./install.sh` in the repo root builds the binary, installs the user service and registers the MCP
server. This directory holds the unit template it renders; nothing here is copied by hand.

## Unit

`uirefs.service` is a template. The installer replaces the command line with the real binary path,
data directory and port, and writes the result to
`${XDG_CONFIG_HOME:-~/.config}/systemd/user/<UIREFS_UNIT>.service` (default unit name `uirefs`).
The unit sets `Restart=on-failure` and `RestartSec=2`, `systemctl --user enable` runs it at login, and
the installer enables linger for the current user
(`loginctl enable-linger "$USER"`, no sudo) so it keeps running after logout.

The data directory holds the SQLite file and every asset; it is the only state, so backing uirefs up
means copying `${UIREFS_DATA:-~/.local/share/uirefs}`.

On macOS the installer writes a `launchd` agent to `~/Library/LaunchAgents/<UIREFS_UNIT>.plist` with
the same command line and `KeepAlive` on a failed exit. That path is best effort and untested.

## Install, check, update

```sh
./install.sh                                                    # build, service, health, MCP
systemctl --user status uirefs
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:4880/api/references
systemd-analyze --user verify ~/.config/systemd/user/uirefs.service
./install.sh                                                    # after git pull: rebuild, restart
./uninstall.sh                                                  # service, MCP entries, binary
./uninstall.sh --purge-data                                     # ... and the data directory
```

## Logs

```sh
journalctl --user -u uirefs -f
```

## Exposing it

There is no auth. Keep it on `127.0.0.1` or a private network: publishing it (a tailnet name, a proxy)
is the owner's step. If agents should use that URL, set `UIREFS_PUBLIC_URL` when you install: the
installer prints it, registers `<UIREFS_PUBLIC_URL>/mcp` in the MCP clients, passes it to the service (which
then accepts that host name; other names get a 403) and leaves the service on localhost. For more names set
`UIREFS_ALLOWED_HOSTS` (comma separated) in the unit.

## Optional embeddings

The unit template has a commented `Environment=UIREFS_EMBED_URL` line. Pass the variable to the
installer (`UIREFS_EMBED_URL=… ./install.sh`) and it is rendered into the unit; or add it later:

```sh
systemctl --user edit uirefs        # add: [Service] Environment=UIREFS_EMBED_URL=http://127.0.0.1:48900/v1/embeddings
systemctl --user restart uirefs

# then embed what is already stored
UIREFS_EMBED_URL=http://127.0.0.1:48900/v1/embeddings \
  ~/.local/bin/uirefs reindex --data ~/.local/share/uirefs
```

Embeddings are optional: with the variable unset nothing about them is loaded, and with the sidecar
down search falls back to keywords. `sidecar/README.md` starts the sidecar.
