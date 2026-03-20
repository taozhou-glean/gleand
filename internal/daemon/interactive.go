package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/peterh/liner"

	"github.com/nickolasclarke/gleand/internal/client"
	"github.com/nickolasclarke/gleand/internal/tools"
)

func historyPath() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(configDir, "gleand", "history")
}

func (d *Daemon) RunInteractiveWithChatID(ctx context.Context, chatID string) error {
	d.resumeChatID = chatID
	return d.RunInteractive(ctx)
}

func (d *Daemon) RunInteractive(ctx context.Context) error {
	if d.cfg.AuthToken == "" {
		return fmt.Errorf("auth token is required for interactive mode (set via -token flag or GLEAN_AUTH_TOKEN env)")
	}

	d.printBanner()

	line := liner.NewLiner()
	defer line.Close()
	line.SetCtrlCAborts(true)
	line.SetMultiLineMode(false)

	if f, err := os.Open(historyPath()); err == nil {
		line.ReadHistory(f)
		f.Close()
	}

	defer func() {
		os.MkdirAll(filepath.Dir(historyPath()), 0o755)
		if f, err := os.Create(historyPath()); err == nil {
			line.WriteHistory(f)
			f.Close()
		}
	}()

	chatID := d.resumeChatID
	if chatID != "" {
		fmt.Printf("Resuming chat: %s\n", chatID)
	}

	for {
		fmt.Println()
		input, err := line.Prompt("gleand> ")
		if err == liner.ErrPromptAborted || err == io.EOF {
			fmt.Println("Goodbye.")
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading input: %w", err)
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		line.AppendHistory(input)

		switch input {
		case "/quit", "/exit":
			fmt.Println("Goodbye.")
			return nil
		case "/new":
			chatID = ""
			fmt.Println("Started new chat session.")
			continue
		case "/tools":
			d.printTools()
			continue
		case "/help":
			d.printHelp()
			continue
		case "/id":
			if chatID == "" {
				fmt.Println("No active chat session.")
			} else {
				fmt.Printf("Chat ID: %s\n", chatID)
			}
			continue
		case "/debug on":
			d.cfg.Debug = true
			d.logger = slog.New(NewDebugLogHandler(os.Stderr))
			fmt.Println("Debug mode enabled.")
			continue
		case "/debug off":
			d.cfg.Debug = false
			d.logger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
			fmt.Println("Debug mode disabled.")
			continue
		case "/debug":
			fmt.Printf("Debug mode: %s\n", map[bool]string{true: "on", false: "off"}[d.cfg.Debug])
			continue
		}

		newChatID, err := d.sendChat(ctx, chatID, input)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n[error] %s\n", err)
			continue
		}
		if newChatID != "" {
			chatID = newChatID
		}
	}
}

func (d *Daemon) sendChat(ctx context.Context, chatID, message string) (string, error) {
	clientTools := d.buildClientTools()

	req := client.ChatRequest{
		ChatID: chatID,
		Messages: []client.ChatMessage{
			{
				Author:    "USER",
				Fragments: []client.ChatMessageFragment{{Text: message}},
				Platform:  "DESKTOP",
			},
		},
		SaveChat:    true,
		ClientTools: clientTools,
		Sc:          d.cfg.ScParams,
		Stream:      true,
	}

	return d.streamAndHandle(ctx, req, 0)
}

func (d *Daemon) streamAndHandle(ctx context.Context, req client.ChatRequest, round int) (string, error) {

	if d.cfg.Debug {
		reqJSON, _ := json.MarshalIndent(req, "", "  ")
		d.logger.Debug("sending chat request", "round", round, "payload", string(reqJSON))
	}

	respCh, errCh := d.getChatClient().StreamChatRequest(req)

	var lastChatID string
	var allText strings.Builder
	var toolRequests []client.ClientToolUseRequest
	var lastAgentConfig map[string]any

	for resp := range respCh {
		if resp.ChatID != "" {
			lastChatID = resp.ChatID
		}

		for _, msg := range resp.Messages {
			if d.cfg.Debug {
				msgJSON, _ := json.Marshal(msg)
				d.logger.Debug("received message", "author", msg.Author, "type", msg.MessageType, "raw", string(msgJSON))
			}
			for _, f := range msg.Fragments {
				if f.Text != "" {
					fmt.Print(f.Text)
					allText.WriteString(f.Text)
				}
				if f.ToolUse != nil {
					d.logger.Debug("received tool request", "tool_id", f.ToolUse.ToolID, "run_id", f.ToolUse.RunID)
					toolRequests = append(toolRequests, *f.ToolUse)
				}
			}
			if msg.AgentConfig != nil {
				lastAgentConfig = msg.AgentConfig
			}
		}
	}

	if err := <-errCh; err != nil {
		return lastChatID, err
	}

	if len(toolRequests) == 0 {
		if allText.Len() > 0 {
			fmt.Println()
		}
		return lastChatID, nil
	}

	fmt.Println()
	results := d.executeToolRequests(toolRequests)

	resultFragments := make([]client.ChatMessageFragment, len(results))
	for i, r := range results {
		r := r
		resultFragments[i] = client.ChatMessageFragment{
			ToolUseResult: &r,
		}
	}

	followUpReq := client.ChatRequest{
		ChatID: lastChatID,
		Messages: []client.ChatMessage{
			{
				Author:      "USER",
				Fragments:   resultFragments,
				MessageType: "TOOL_USE",
				Platform:    "DESKTOP",
			},
		},
		MessageType: "TOOL_USE",
		AgentConfig: lastAgentConfig,
		SaveChat:    true,
		ClientTools: d.buildClientTools(),
		Sc:          d.cfg.ScParams,
		Stream:      true,
	}

	return d.streamAndHandle(ctx, followUpReq, round+1)
}

func (d *Daemon) executeToolRequests(requests []client.ClientToolUseRequest) []client.ClientToolUseResult {
	results := make([]client.ClientToolUseResult, 0, len(requests))

	for _, req := range requests {
		fmt.Printf("\n[tool] %s (runId: %s)\n", req.ToolID, req.RunID)

		if req.Parameters != nil {
			paramsJSON, _ := json.MarshalIndent(req.Parameters, "       ", "  ")
			fmt.Printf("       params: %s\n", string(paramsJSON))
		}

		result, err := d.registry.Execute(req.ToolID, req.Parameters)

		var output map[string]any
		if err != nil {
			fmt.Printf("       [error] %s\n", err)
			output = map[string]any{"error": err.Error()}
		} else {
			output = result.Output

			outputJSON, _ := json.MarshalIndent(output, "       ", "  ")
			outputStr := string(outputJSON)
			if len(outputStr) > 500 {
				outputStr = outputStr[:500] + "... [truncated]"
			}
			fmt.Printf("       result: %s\n", outputStr)
		}

		results = append(results, client.ClientToolUseResult{
			RunID:  req.RunID,
			ToolID: req.ToolID,
			Output: output,
		})
	}

	fmt.Println("\n[sending tool results back to assistant...]")
	return results
}

func (d *Daemon) buildClientTools() []client.ClientTool {
	defs := d.registry.Definitions()
	clientTools := make([]client.ClientTool, len(defs))
	for i, def := range defs {
		clientTools[i] = client.ClientTool{
			ID:          def.ToolID,
			Name:        def.Name,
			Description: def.Description,
			InputSchema: toolSchemaToMap(def.InputSchema),
		}
	}
	return clientTools
}

func toolSchemaToMap(schema tools.ToolSchema) map[string]any {
	properties := make(map[string]any, len(schema.Properties))
	for name, prop := range schema.Properties {
		p := map[string]any{"type": prop.Type}
		if prop.Description != "" {
			p["description"] = prop.Description
		}
		if prop.Default != nil {
			p["default"] = prop.Default
		}
		properties[name] = p
	}
	m := map[string]any{
		"type":       schema.Type,
		"properties": properties,
	}
	if len(schema.Required) > 0 {
		m["required"] = schema.Required
	}
	return m
}

func (d *Daemon) getChatClient() *client.ChatClient {
	return d.chatClient
}

func (d *Daemon) printBanner() {
	fmt.Println("╔══════════════════════════════════════════════╗")
	fmt.Println("║            gleand interactive mode           ║")
	fmt.Println("╠══════════════════════════════════════════════╣")
	fmt.Printf("║  Backend:  %-33s║\n", d.cfg.Backend)
	fmt.Printf("║  Device:   %-33s║\n", truncateStr(d.cfg.DeviceName, 33))
	fmt.Printf("║  Tools:    %-33s║\n", fmt.Sprintf("%d registered", len(d.registry.Definitions())))
	if d.cfg.ScParams != "" {
		fmt.Printf("║  SC:       %-33s║\n", truncateStr(d.cfg.ScParams, 33))
	}
	if d.cfg.Debug {
		fmt.Printf("║  Debug:    %-33s║\n", "enabled (yellow logs on stderr)")
	}
	fmt.Println("╠══════════════════════════════════════════════╣")
	fmt.Println("║  /tools  - list tools    /new  - new chat   ║")
	fmt.Println("║  /id     - show chat id  /quit - exit       ║")
	fmt.Println("╚══════════════════════════════════════════════╝")
}

func (d *Daemon) printTools() {
	fmt.Println("\nRegistered tools:")
	for _, def := range d.registry.Definitions() {
		fmt.Printf("  %-28s %s\n", def.ToolID, def.Description)
	}
}

func (d *Daemon) printHelp() {
	fmt.Println("\nCommands:")
	fmt.Println("  /tools      - List registered tools")
	fmt.Println("  /new        - Start a new chat session")
	fmt.Println("  /id         - Show current chat ID")
	fmt.Println("  /debug      - Show debug mode status")
	fmt.Println("  /debug on   - Enable debug logging")
	fmt.Println("  /debug off  - Disable debug logging")
	fmt.Println("  /quit       - Exit")
	fmt.Println()
	fmt.Println("Type any message to send to Glean Assistant.")
	fmt.Println("If the assistant wants to use a local tool, it will")
	fmt.Println("be executed automatically and results sent back.")
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
