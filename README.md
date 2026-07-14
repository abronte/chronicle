# Chronicle

A system-wide file change tracker. Chronicle watches the directories you
configure, stores text-file changes in a central Turso database, and includes a
web UI for managing watched directories, global ignore patterns, and history.

## Quick start

Install the latest release:

```sh
curl -fsSL https://raw.githubusercontent.com/abronte/chronicle/refs/heads/main/scripts/install.sh | bash
```

By default this installs `chronicle` to `$HOME/.local/bin`. To install somewhere
else, set `CHRONICLE_INSTALL_DIR`:

```sh
curl -fsSL https://raw.githubusercontent.com/abronte/chronicle/refs/heads/main/scripts/install.sh | CHRONICLE_INSTALL_DIR=/usr/local/bin bash
```

Install and start Chronicle as a macOS service:

```sh
scripts/install-macos-service.sh
```

The service is installed as a user LaunchAgent, starts immediately, and starts
again at login. It defaults to `chronicle watch`; pass Chronicle arguments after
`--` to override that:

```sh
scripts/install-macos-service.sh -- --addr :12346
```

```sh
# Watch configured directories and serve the web UI
go run ./cmd/chronicle
```

```sh
# Print the version
go run ./cmd/chronicle --version
```

```sh
# Build a binary
go build -o bin/chronicle ./cmd/chronicle
```

## Commands

### `watch` (default)

Starts file watchers for every directory listed in
`~/.config/chronicle/config.toml` and serves the web UI on port `12345`.
Chronicle records ASCII text file changes under 5 MB and keeps the last
captured content when a tracked file is deleted. Hidden directories
(`.`-prefixed), paths ignored by each root's `.gitignore`, and paths matching
global `ignore_patterns` from `config.toml` are skipped.

```
chronicle watch
```

On each change it prints the short SHA-256 hash and file path.

### `web`

Serves the web UI without starting the watcher.

```
chronicle web
```

Open <http://localhost:12345> to list, add, and delete monitored directories
and global ignore patterns, view chronological change history for a directory,
and inspect per-file diffs.

### `recent`

Shows the 10 most recently changed files across the central history database.

```
chronicle recent
```

### `diffs`

Shows the last 5 diffs for a given file.

```
chronicle diffs path/to/file.go
chronicle diffs -dir /path/to/root path/to/file.go
```

### `restore`

Restores the latest recorded contents for a file. Relative paths are resolved
from the current working directory, so the restored file is matched to its
exact monitored path even when multiple roots contain the same relative file.

```
chronicle restore path/to/file.go
```

Use `-version` with a full SHA or unique SHA prefix shown by the web UI or
`diffs` command to restore a specific version:

```
chronicle restore -version a1b2c3d4 path/to/file.go
```

Chronicle recreates missing parent directories and atomically replaces an
existing target. The command uses the running local web service when available
so it works while the watcher holds the history database open. If the watcher
uses a custom web address, pass the same address to restore (development uses
port `12346`). Restart a watcher that was started by an older Chronicle version
once after upgrading so the restore API is available:

```
chronicle restore -addr :12346 path/to/file.go
```

### `update`

Downloads the latest release from GitHub and replaces the current binary.

```
chronicle update
```

### `help`

Prints usage information.

```
chronicle help
```

## Data storage

Chronicle stores system-wide state under `~/.config/chronicle`:

- `config.toml`: TOML config containing monitored directories and global
  ignore patterns.
- `history.db`: central Turso database containing file-change history.

The web assets are embedded with Go's `embed` package, so Chronicle ships as a
single binary.

## Build & development

```sh
make build   # go build -o bin/chronicle ./cmd/chronicle
make run     # go run ./cmd/chronicle
make test    # go test ./...
make fmt     # gofmt -w .
make vet     # go vet ./...
make clean   # rm -rf bin/
```
