# gleand

A small Go daemon for bridging **Glean Assistant** with **local desktop tool execution**.

`gleand` runs on the user's machine and executes approved local tools like shell commands, file reads, file writes, directory listing, and system info. The current design uses Glean's existing chat API and a stop-and-resume tool execution flow.

## What it does

- Runs as a local daemon on the user's machine
- Bridges assistant tool calls to local execution
- Supports read and write style desktop tools
- Includes an interactive mode for end-to-end testing
- Can resume an existing chat session with `--chat-id`

## Included tools

- `desktop_run_command`
- `desktop_read_file`
- `desktop_write_file`
- `desktop_list_directory`
- `desktop_system_info`

## Project layout

```text
cmd/gleand/          main entry point
internal/client/     chat API types and client code
internal/config/     config loading
internal/daemon/     daemon lifecycle and interactive loop
internal/tools/      desktop tool implementations
electron/            Electron-side integration helpers
DESIGN.md            architecture and protocol notes
```

## Build

```bash
make build
```

This creates a local `gleand` binary in the repo root.

## Cross-build

```bash
make cross-build
```

Artifacts are written to `dist/`.

## Run

```bash
./gleand --help
```

Useful flags:

- `--backend` override backend URL
- `--token` provide auth token
- `--sc` pass chat API sc params
- `--interactive` run interactive REPL mode
- `--chat-id` resume an existing chat session
- `--debug` enable debug logging
- `--list-tools` print registered tool definitions
- `--config-path` print config path
- `--version` print version

You can also provide `GLEAN_AUTH_TOKEN` via env.

## Notes

- The repo currently ignores the root `gleand` binary and `dist/` build artifacts.
- The architecture and protocol details live in [`DESIGN.md`](./DESIGN.md).

## Status

Early prototype / exploration repo.
