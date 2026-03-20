import { ChildProcess, spawn } from 'child_process'
import { createInterface, Interface } from 'readline'

import type {
  DaemonConfig,
  DaemonHeartbeat,
  DaemonMessage,
  DaemonToolDefinition,
  DaemonToolRequest,
  DaemonToolResult,
} from './types'

const RESTART_DELAY_MS = 3000
const MAX_RESTART_ATTEMPTS = 5
const RESTART_WINDOW_MS = 60000

export class DaemonService {
  private process: ChildProcess | null = null
  private readline: Interface | null = null
  private config: DaemonConfig
  private restartAttempts: number = 0
  private lastRestartTime: number = 0
  private pendingRequests: Map<string, { resolve: (result: DaemonToolResult) => void; timer: NodeJS.Timeout }> =
    new Map()
  private registeredTools: DaemonToolDefinition[] = []
  private deviceId: string = ''
  private isShuttingDown: boolean = false

  constructor(config: DaemonConfig) {
    this.config = config
  }

  async start(): Promise<void> {
    if (this.process) {
      return
    }

    const args: string[] = []
    if (this.config.backend) {
      args.push('-backend', this.config.backend)
    }

    const env = { ...process.env }
    if (this.config.authToken) {
      env.GLEAN_AUTH_TOKEN = this.config.authToken
    }

    this.process = spawn(this.config.binaryPath, args, {
      stdio: ['pipe', 'pipe', 'pipe'],
      env,
    })

    this.readline = createInterface({ input: this.process.stdout! })
    this.readline.on('line', (line: string) => this.handleLine(line))

    this.process.stderr?.on('data', (data: Buffer) => {
      const lines = data.toString().split('\n').filter(Boolean)
      for (const line of lines) {
        try {
          const logEntry = JSON.parse(line)
          if (logEntry.level === 'ERROR') {
            this.config.onError?.(new Error(logEntry.msg))
          }
        } catch {
          // non-JSON stderr line, ignore
        }
      }
    })

    this.process.on('exit', (code) => {
      this.process = null
      this.readline = null
      this.config.onExit?.(code)

      if (!this.isShuttingDown) {
        this.attemptRestart()
      }
    })

    this.process.on('error', (err) => {
      this.config.onError?.(err)
    })
  }

  stop(): void {
    this.isShuttingDown = true

    for (const [runId, { timer }] of this.pendingRequests) {
      clearTimeout(timer)
      this.pendingRequests.delete(runId)
    }

    if (this.process) {
      this.process.kill('SIGTERM')

      setTimeout(() => {
        if (this.process) {
          this.process.kill('SIGKILL')
        }
      }, 5000)
    }
  }

  updateAuthToken(token: string): void {
    this.config.authToken = token
    // The daemon reads the token at startup via env var.
    // For a running daemon, we'd need to restart it or send a config update.
    // For now, restart on token change.
    if (this.process) {
      this.stop()
      this.isShuttingDown = false
      this.start()
    }
  }

  async executeToolRequest(request: DaemonToolRequest, timeoutMs: number = 60000): Promise<DaemonToolResult> {
    if (!this.process?.stdin?.writable) {
      throw new Error('Daemon is not running')
    }

    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pendingRequests.delete(request.runId)
        reject(new Error(`Tool execution timed out after ${timeoutMs}ms`))
      }, timeoutMs)

      this.pendingRequests.set(request.runId, { resolve, timer })

      const line = JSON.stringify(request) + '\n'
      this.process!.stdin!.write(line, (err) => {
        if (err) {
          clearTimeout(timer)
          this.pendingRequests.delete(request.runId)
          reject(new Error(`Failed to send request to daemon: ${err.message}`))
        }
      })
    })
  }

  getRegisteredTools(): DaemonToolDefinition[] {
    return this.registeredTools
  }

  getDeviceId(): string {
    return this.deviceId
  }

  isRunning(): boolean {
    return this.process !== null
  }

  private handleLine(line: string): void {
    let message: DaemonMessage
    try {
      message = JSON.parse(line)
    } catch {
      return
    }

    switch (message.type) {
      case 'heartbeat':
        this.handleHeartbeat(message)
        break
      case 'tool_result':
        this.handleToolResult(message)
        break
    }
  }

  private handleHeartbeat(heartbeat: DaemonHeartbeat): void {
    this.deviceId = heartbeat.deviceId
    this.registeredTools = heartbeat.tools
    this.config.onHeartbeat?.(heartbeat)
  }

  private handleToolResult(result: DaemonToolResult): void {
    const pending = this.pendingRequests.get(result.runId)
    if (pending) {
      clearTimeout(pending.timer)
      this.pendingRequests.delete(result.runId)
      pending.resolve(result)
    }
    this.config.onToolResult?.(result)
  }

  private attemptRestart(): void {
    const now = Date.now()

    if (now - this.lastRestartTime > RESTART_WINDOW_MS) {
      this.restartAttempts = 0
    }

    if (this.restartAttempts >= MAX_RESTART_ATTEMPTS) {
      this.config.onError?.(new Error(`Daemon crashed ${MAX_RESTART_ATTEMPTS} times in ${RESTART_WINDOW_MS}ms, giving up`))
      return
    }

    this.restartAttempts++
    this.lastRestartTime = now

    const delay = RESTART_DELAY_MS * Math.pow(2, this.restartAttempts - 1)
    setTimeout(() => {
      if (!this.isShuttingDown) {
        this.start()
      }
    }, delay)
  }
}
