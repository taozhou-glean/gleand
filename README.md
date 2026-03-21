# gleand

A small Go daemon for bridging **Glean Assistant** with **local desktop tool execution**.

`gleand` runs on the user's machine and executes approved local tools like shell commands, file reads, file writes, directory listing, and system info. It supports both:

- **stdin/stdout daemon mode** for Electron or other host processes
- **interactive REPL mode** for local end-to-end testing

## What it does

- Runs local desktop tools on the user's machine
- Bridges assistant tool calls to local execution
- Supports both daemon mode and interactive chat mode
- Can resume an existing chat session with `--chat-id`
- Supports browser-based OAuth login in interactive mode
- Persists chat history, config, and OAuth tokens under the user's config directory

## Included tools

- `desktop_run_command`
- `desktop_read_file`
- `desktop_write_file`
- `desktop_list_directory`
- `desktop_system_info`

## Project layout

```text
cmd/gleand/          main entry point
internal/client/     chat API types, chat client, OAuth client
internal/config/     config loading and defaults
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

To stamp an explicit version into the binary:

```bash
make build VERSION=0.0.2
```

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
- `--token` provide auth token directly
- `--sc` pass chat API sc params
- `--interactive` run interactive REPL mode
- `--chat-id` resume an existing chat session
- `--debug` enable debug logging
- `--list-tools` print registered tool definitions
- `--config-path` print config path
- `--version` print version

Auth token precedence is:

1. `--token`
2. config file value
3. `GLEAN_AUTH_TOKEN`
4. saved OAuth token restored in interactive mode

If `--sc` is not provided and the config file does not define it, `gleand` applies a built-in default SC string for local testing.

## Interactive mode

Run:

```bash
./gleand --interactive
```

If no auth token is available, interactive mode prompts you to use `/auth`.

### Interactive commands

- `/help` show help
- `/tools` list registered tools
- `/new` start a new chat session
- `/id` show the current chat ID
- `/model` show the current model
- `/model list` list available models
- `/model <ID>` switch to a model
- `/sc` show current sc params
- `/sc <params>` set sc params
- `/sc clear` clear sc params
- `/auth` authenticate via browser OAuth
- `/auth status` show token status
- `/auth logout` clear the saved token
- `/debug` show debug mode status
- `/debug on` enable debug logging
- `/debug off` disable debug logging
- `/quit` exit interactive mode
- `/exit` exit interactive mode

Any non-slash input is sent to Glean Assistant. If the assistant requests a local tool, `gleand` executes it and sends the result back automatically.

### Multiline input

Interactive input supports multiline messages. End a line with `\` to continue:

```text
gleand> summarize this change \
   ...> and explain the tradeoffs
```

## OAuth login flow

In interactive mode, `/auth`:

1. Dynamically registers a local OAuth client
2. Opens the browser to authenticate
3. Receives the callback on `http://localhost:<random-port>/callback`
4. Stores the returned token locally
5. Switches chat requests to the REST chat path used by the OAuth flow

Saved tokens include refresh-token metadata when available, and interactive mode attempts token restore on startup.

## Config and local state

Print the config path with:

```bash
./gleand --config-path
```

By default, local state is stored under the user's config directory:

- `gleand/config.json` — daemon config
- `gleand/token.json` — saved OAuth token
- `gleand/history` — interactive command history

## Daemon mode

Without `--interactive`, `gleand` runs as a stdin/stdout daemon intended for host processes such as Electron.

Protocol shape:

- **stdin**: one JSON tool request per line
- **stdout**: one JSON tool result per line
- periodic heartbeat messages are also emitted on stdout

See [`DESIGN.md`](./DESIGN.md) for protocol and architecture details.

## Notes

- The root `gleand` binary and `dist/` build artifacts are local build outputs.
- The architecture and protocol details live in [`DESIGN.md`](./DESIGN.md).

## Status

Early prototype / exploration repo.
