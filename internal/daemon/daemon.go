package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/nickolasclarke/gleand/internal/client"
	"github.com/nickolasclarke/gleand/internal/config"
	"github.com/nickolasclarke/gleand/internal/tools"
)

type Daemon struct {
	cfg        *config.Config
	chatClient *client.ChatClient
	registry   *tools.Registry
	logger     *slog.Logger

	mu             sync.Mutex
	activeSessions map[string]*sessionState
	resumeChatID   string
	modelSetID     string
	cachedModels   []client.ModelSet
}

type sessionState struct {
	ChatID      string
	AgentConfig map[string]any
	LastSeen    time.Time
}

func New(cfg *config.Config, logger *slog.Logger) *Daemon {
	chatClient := client.NewChatClient(cfg.Backend, cfg.AuthToken, logger)
	registry := tools.NewRegistry()

	registry.Register(tools.NewRunCommandTool(cfg.MaxCommandTimeout, cfg.BlockedCommands))
	registry.Register(tools.NewReadFileTool(cfg.AllowedPaths))
	registry.Register(tools.NewWriteFileTool(cfg.AllowedPaths))
	registry.Register(tools.NewListDirectoryTool(cfg.AllowedPaths))
	registry.Register(tools.NewSystemInfoTool())

	return &Daemon{
		cfg:            cfg,
		chatClient:     chatClient,
		registry:       registry,
		logger:         logger,
		activeSessions: make(map[string]*sessionState),
	}
}

func (d *Daemon) ToolDefinitions() []tools.ToolDefinition {
	return d.registry.Definitions()
}

func (d *Daemon) Run(ctx context.Context) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	d.logger.Info("gleand starting",
		"device_id", d.cfg.DeviceID,
		"device_name", d.cfg.DeviceName,
		"backend", d.cfg.Backend,
		"tools", len(d.registry.Definitions()),
	)

	for _, def := range d.registry.Definitions() {
		d.logger.Info("registered tool", "tool_id", def.ToolID, "name", def.Name)
	}

	d.logger.Info("daemon ready, waiting for tool execution requests")
	d.logger.Info("mode: stdin listener (reads JSON tool requests from stdin)")

	return d.runStdinLoop(ctx)
}

// runStdinLoop reads tool execution requests from stdin and writes results to stdout.
// This is the primary IPC mechanism when the daemon is spawned by Electron.
//
// Protocol:
//
//	Input  (one JSON per line): {"chatId":"...","runId":"...","toolId":"...","parameters":{...},"agentConfig":{...}}
//	Output (one JSON per line): {"chatId":"...","runId":"...","toolId":"...","output":{...}}
//	Heartbeat output:           {"type":"heartbeat","deviceId":"...","tools":[...]}
func (d *Daemon) runStdinLoop(ctx context.Context) error {
	heartbeatTicker := time.NewTicker(time.Duration(d.cfg.HeartbeatIntervalSeconds) * time.Second)
	defer heartbeatTicker.Stop()

	d.emitHeartbeat()

	inputCh := make(chan []byte, 10)
	go d.readStdin(inputCh)

	for {
		select {
		case <-ctx.Done():
			d.logger.Info("shutting down")
			return nil

		case <-heartbeatTicker.C:
			d.emitHeartbeat()

		case line, ok := <-inputCh:
			if !ok {
				d.logger.Info("stdin closed, shutting down")
				return nil
			}
			d.handleStdinMessage(ctx, line)
		}
	}
}

type stdinRequest struct {
	ChatID      string         `json:"chatId"`
	RunID       string         `json:"runId"`
	ToolID      string         `json:"toolId"`
	Parameters  map[string]any `json:"parameters"`
	AgentConfig map[string]any `json:"agentConfig"`
}

type stdoutResponse struct {
	Type     string         `json:"type"`
	ChatID   string         `json:"chatId,omitempty"`
	RunID    string         `json:"runId,omitempty"`
	ToolID   string         `json:"toolId,omitempty"`
	Output   map[string]any `json:"output,omitempty"`
	Error    string         `json:"error,omitempty"`
	DeviceID string         `json:"deviceId,omitempty"`
	Tools    any            `json:"tools,omitempty"`
}

func (d *Daemon) readStdin(ch chan<- []byte) {
	defer close(ch)
	buf := make([]byte, 0, 1024*64)
	tmp := make([]byte, 4096)

	for {
		n, err := os.Stdin.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			for {
				idx := -1
				for i, b := range buf {
					if b == '\n' {
						idx = i
						break
					}
				}
				if idx < 0 {
					break
				}
				line := make([]byte, idx)
				copy(line, buf[:idx])
				buf = buf[idx+1:]
				if len(line) > 0 {
					ch <- line
				}
			}
		}
		if err != nil {
			return
		}
	}
}

func (d *Daemon) handleStdinMessage(_ context.Context, line []byte) {
	var req stdinRequest
	if err := json.Unmarshal(line, &req); err != nil {
		d.logger.Error("invalid JSON on stdin", "error", err, "line", string(line))
		return
	}

	if req.ToolID == "" || req.RunID == "" {
		d.logger.Error("missing toolId or runId in request", "request", string(line))
		return
	}

	d.logger.Info("executing tool", "tool_id", req.ToolID, "run_id", req.RunID, "chat_id", req.ChatID)

	result, err := d.registry.Execute(req.ToolID, req.Parameters)

	resp := stdoutResponse{
		Type:   "tool_result",
		ChatID: req.ChatID,
		RunID:  req.RunID,
		ToolID: req.ToolID,
	}

	if err != nil {
		resp.Error = err.Error()
		resp.Output = map[string]any{"error": err.Error()}
	} else {
		resp.Output = result.Output
	}

	d.emit(resp)

	if req.ChatID != "" {
		go d.sendResultToBackend(req, resp)
	}
}

func (d *Daemon) sendResultToBackend(req stdinRequest, resp stdoutResponse) {
	toolResult := client.ClientToolUseResult{
		RunID:  req.RunID,
		ToolID: req.ToolID,
		Output: resp.Output,
	}

	_, err := d.chatClient.SendToolResults(req.ChatID, []client.ClientToolUseResult{toolResult}, req.AgentConfig)
	if err != nil {
		d.logger.Error("failed to send result to backend", "error", err, "chat_id", req.ChatID, "run_id", req.RunID)
	} else {
		d.logger.Info("result sent to backend", "chat_id", req.ChatID, "run_id", req.RunID)
	}
}

func (d *Daemon) emitHeartbeat() {
	d.emit(stdoutResponse{
		Type:     "heartbeat",
		DeviceID: d.cfg.DeviceID,
		Tools:    d.registry.Definitions(),
	})
}

func (d *Daemon) emit(resp stdoutResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		d.logger.Error("failed to marshal response", "error", err)
		return
	}
	fmt.Fprintln(os.Stdout, string(data))
}
