# Glean Desktop Daemon — Architecture & Protocol Design

## Overview

A daemon process that runs on the user's machine and acts as a bridge between
Glean Assistant and local tool execution. When the assistant determines it needs
to run something locally (shell command, file read, etc.), it dispatches a tool
call to the daemon, which executes it and returns the result.

## Key Architectural Insight

The Glean assistant uses a **stop-and-resume** pattern for client tool execution:

1. **LLM selects a tool** → `ClientTool._execute()` fires on the server
2. **Server sends `ClientToolExecutionRequest`** down the streaming response
3. **Agent loop terminates** (via `StopAtTools` — the tool is in `tools_requiring_approval`, so the loop treats output as final)
4. **Client executes the tool** and sends a **new chat message** back with the result
5. **Agent loop restarts** with the tool result injected into the conversation history

The agent loop does NOT suspend — it fully terminates. The "resume" is a fresh
execution where `AgenticLoopSession.from_context` finds the tool output in
`event_history` and injects it into the message history.

## Protocol: Reusing the Existing Chat API

The daemon reuses the existing `/api/v1/chat` REST API. No new backend endpoints
are needed for the initial implementation.

### Flow

```
User (Web/Mobile/Desktop)
    │
    │  1. "Run `ls -la` on my machine"
    │     POST /api/v1/chat  (ChatRequest)
    ▼
┌─────────────────────────────────────────────────┐
│  Glean Backend (QE → PyAgents)                  │
│                                                  │
│  LLM decides: use tool "desktop_run_command"     │
│  → ClientTool._execute() fires                   │
│  → Sends ClientToolExecutionRequest in response  │
│  → Agent loop terminates                         │
└──────────────────┬──────────────────────────────┘
                   │
                   │  2. Streaming ChatResponse contains:
                   │     { "toolUse": { "toolId": "desktop_run_command",
                   │       "runId": "call_abc123",
                   │       "parameters": { "command": "ls -la" } } }
                   ▼
┌─────────────────────────────────────────────────┐
│  Client (Web UI / Electron)                      │
│                                                  │
│  Detects toolUse fragment in response            │
│  Option A: Execute locally (if Electron)         │
│  Option B: Forward to daemon via IPC             │
│  Option C: Daemon is polling / connected and     │
│            picks it up directly                  │
└──────────────────┬──────────────────────────────┘
                   │
                   │  3. Tool executed locally
                   ▼
┌─────────────────────────────────────────────────┐
│  Daemon                                          │
│                                                  │
│  Receives: { toolId, runId, parameters }         │
│  Executes: run_command("ls -la")                 │
│  Returns:  { output: { stdout: "...", rc: 0 } }  │
└──────────────────┬──────────────────────────────┘
                   │
                   │  4. Send result back
                   │     POST /api/v1/chat  (ChatRequest with toolUseResult fragment)
                   │     { "fragments": [{ "toolUseResult": {
                   │         "runId": "call_abc123",
                   │         "toolId": "desktop_run_command",
                   │         "output": { "stdout": "...", "rc": 0 }
                   │     }}] }
                   ▼
┌─────────────────────────────────────────────────┐
│  Glean Backend                                   │
│                                                  │
│  Agent loop restarts with tool result in history │
│  LLM sees the output, generates final response   │
└─────────────────────────────────────────────────┘
```

## Daemon Architecture

### Connection Mode: Polling + WebSocket Upgrade

For the initial implementation, the daemon uses **polling** against the chat API
to check for pending tool execution requests. This avoids any backend changes.

**Phase 1 (Polling)**:
- Daemon polls `GET /api/v1/chat/{chatId}/messages` for new messages
- When it sees a `toolUse` fragment in the latest assistant message, it executes
- Sends result back via `POST /api/v1/chat`

**Phase 2 (WebSocket/SSE)**:
- Daemon maintains a persistent connection to receive push notifications
- Backend pushes `ClientToolExecutionRequest` directly to the daemon
- Much lower latency, but requires a new backend endpoint

### Tool Registration

Tools are registered via the existing `WriteToolConfiguration` mechanism.
On startup, the daemon registers its tools with the backend:

```
POST /api/v1/tools/register
{
  "deviceId": "device_abc123",
  "deviceName": "Tao's MacBook Pro",
  "tools": [
    {
      "name": "run_command",
      "toolId": "desktop_run_command",
      "description": "Execute a shell command on the user's local machine and return stdout/stderr/exit code",
      "input_schema": {
        "type": "object",
        "properties": {
          "command": { "type": "string", "description": "Shell command to execute" },
          "working_directory": { "type": "string", "description": "Working directory for the command" },
          "timeout_seconds": { "type": "integer", "description": "Timeout in seconds", "default": 30 }
        },
        "required": ["command"]
      }
    }
  ]
}
```

### Multi-Device Support

Each daemon instance registers with a unique `deviceId` (machine UUID + user).
The backend maintains a registry of active devices per user.

**Routing Strategy**:
1. If only one device is online → route to it automatically
2. If multiple devices are online → assistant asks the user which device
3. User can set a "default device" preference
4. Tool calls can include a `targetDeviceId` parameter

**Device Heartbeat**:
- Daemon sends heartbeat every 30s to `/api/v1/devices/heartbeat`
- Backend marks device as offline after 90s of no heartbeat
- Device list available at `GET /api/v1/devices`

## REST API Types (matching existing scio patterns)

### ClientToolUseRequest (server → client)
```json
{
  "toolId": "desktop_run_command",
  "runId": "call_abc123",
  "parameters": {
    "command": "ls -la",
    "timeout_seconds": 30
  }
}
```

### ClientToolUseResult (client → server)
```json
{
  "toolId": "desktop_run_command",
  "runId": "call_abc123",
  "output": {
    "stdout": "total 16\ndrwxr-xr-x  4 tao  staff  128 Mar 19 10:00 .\n",
    "stderr": "",
    "exit_code": 0,
    "duration_ms": 42
  }
}
```

## Security Model

### Approval Flow
1. User types question in Glean Assistant
2. Assistant decides to use a desktop tool
3. Tool request appears in chat UI with "Run" / "Decline" buttons
4. User clicks "Run" → daemon executes
5. OR: User enables "auto-run" mode (existing `mcpAutoRunMode` preference)

### Sandboxing
- Commands run via user's default shell
- Configurable command allowlist/blocklist
- File operations sandboxed to configurable paths (default: $HOME)
- All operations have timeouts (default: 30s, max: 300s)
- No sudo/root access

### Authentication
- Daemon receives OAuth token from Electron app via IPC
- Token refreshed by Electron app, pushed to daemon
- All API calls use `Authorization: Bearer <token>`

## Pre-packaged Tools

| Tool ID | Description | Approval Required |
|---------|-------------|-------------------|
| `desktop_run_command` | Execute shell command | Yes |
| `desktop_read_file` | Read file contents | No (read-only) |
| `desktop_write_file` | Write/create file | Yes |
| `desktop_list_directory` | List directory contents | No (read-only) |
| `desktop_system_info` | Get OS, CPU, memory, disk info | No (read-only) |

## Implementation Plan

### Phase 1: Standalone Daemon (this repo)
- Go binary that connects to Glean backend
- Implements tool execution
- CLI for configuration and testing
- Can run standalone (user provides auth token manually)

### Phase 2: Electron Integration
- Daemon bundled in Electron app via `extraResources`
- Electron manages daemon lifecycle
- Auth token passed via IPC/stdin
- Approval UI in Electron

### Phase 3: Backend Integration
- Dedicated daemon endpoint (WebSocket or SSE)
- Device registry and routing
- Admin controls for enterprise

## File Structure

```
daemon/
├── cmd/
│   └── gleand/          # Main binary entry point
│       └── main.go
├── internal/
│   ├── client/          # Glean API client (chat, auth, device registration)
│   │   ├── chat.go
│   │   ├── auth.go
│   │   └── device.go
│   ├── config/          # Configuration management
│   │   └── config.go
│   ├── executor/        # Tool execution engine
│   │   ├── executor.go
│   │   └── sandbox.go
│   ├── tools/           # Pre-packaged tool implementations
│   │   ├── registry.go
│   │   ├── run_command.go
│   │   ├── read_file.go
│   │   ├── write_file.go
│   │   ├── list_directory.go
│   │   └── system_info.go
│   └── daemon/          # Daemon lifecycle management
│       └── daemon.go
├── electron/            # Electron integration TypeScript
│   ├── daemonService.ts
│   └── types.ts
├── go.mod
├── go.sum
├── Makefile
└── DESIGN.md
```
