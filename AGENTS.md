# AGENTS.md

Guidance for agents working in this repository.

## Project Overview

Chronicle is a Go CLI project. The executable entrypoint lives at `cmd/chronicle`.

## Layout

- `cmd/chronicle/main.go`: CLI entrypoint and command-line flag handling.
- `go.mod`: Go module definition for module `chronicle`.
- `README.md`: Basic usage and build instructions.

## Common Commands

Run the CLI locally:

```sh
go run ./cmd/chronicle
```

Run all tests:

```sh
go test ./...
```

Build the CLI:

```sh
go build -o bin/chronicle ./cmd/chronicle
```

Format Go code:

```sh
gofmt -w ./cmd/chronicle
```

## Development Guidelines

- Keep the CLI simple and standard-library-first unless a dependency is clearly justified.
- Keep command behavior testable by putting logic in functions like `run(args, stdout)` instead of directly in `main`.
- Prefer small, focused changes over broad restructuring.
- Run `gofmt` on changed Go files before finishing.
- Run `go test ./...` after code changes.
- use go-libsql for the database

## Git And Files

- Do not commit generated binaries or build artifacts from `bin/` or `dist/`.
- Do not overwrite unrelated local changes.
- Do not add secrets or machine-local configuration to the repository.
