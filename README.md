# Chronicle

A local file change tracker. Chronicles watches your project files and stores
every change in a SQLite database so you can review history and diffs later.

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

```sh
# Watch for changes (starts the watcher in the current directory)
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

Starts a file watcher that records every ASCII text file change (under 5 MB)
to `.chronicle/history.db`. Hidden directories (`.`-prefixed) are skipped.

```
chronicle watch
```

On each change it prints the short SHA-256 hash and file path.

### `recent`

Shows the 10 most recently changed files (grouped by file path).

```
chronicle recent
```

### `diffs`

Shows the last 5 diffs for a given file.

```
chronicle diffs path/to/file.go
```

### `help`

Prints usage information.

```
chronicle help
```

## Data storage

Chronicle keeps its data in `.chronicle/history.db` inside the directory where
it runs. This is a SQLite database. Add `.chronicle/` to your `.gitignore` if
you don't want to track it.

## Build & development

```sh
make build   # go build -o bin/chronicle ./cmd/chronicle
make run     # go run ./cmd/chronicle
make test    # go test ./...
make fmt     # gofmt -w .
make vet     # go vet ./...
make clean   # rm -rf bin/
```
