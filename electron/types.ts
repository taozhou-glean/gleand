export interface DaemonToolDefinition {
  name: string
  toolId: string
  description: string
  input_schema: {
    type: string
    properties: Record<string, { type: string; description?: string; default?: unknown }>
    required?: string[]
  }
}

export interface DaemonHeartbeat {
  type: 'heartbeat'
  deviceId: string
  tools: DaemonToolDefinition[]
}

export interface DaemonToolResult {
  type: 'tool_result'
  chatId?: string
  runId: string
  toolId: string
  output: Record<string, unknown>
  error?: string
}

export type DaemonMessage = DaemonHeartbeat | DaemonToolResult

export interface DaemonToolRequest {
  chatId?: string
  runId: string
  toolId: string
  parameters: Record<string, unknown>
  agentConfig?: Record<string, unknown>
}

export interface DaemonConfig {
  binaryPath: string
  authToken: string
  backend?: string
  onToolResult?: (result: DaemonToolResult) => void
  onHeartbeat?: (heartbeat: DaemonHeartbeat) => void
  onError?: (error: Error) => void
  onExit?: (code: number | null) => void
}
