package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/peterh/liner"

	"github.com/taozhou/gleand/internal/client"
	"github.com/taozhou/gleand/internal/tools"
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
	d.tryRestoreAuth()

	if len(d.cfg.AuthToken) == 0 {
		fmt.Println("No auth token found. Use /auth to authenticate, or pass --token.")
	}

	d.printBanner()
	d.connectMCPAsync(ctx)

	line := liner.NewLiner()
	defer line.Close()
	defer d.MCP.DisconnectAll()
	line.SetCtrlCAborts(true)
	line.SetMultiLineMode(true)

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
		input, err := d.readMultiLineInput(line)
		if err == liner.ErrPromptAborted || err == io.EOF {
			fmt.Println("Goodbye.")
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading input: %w", err)
		}

		if len(input) == 0 {
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
		case "/model":
			d.printCurrentModel()
			continue
		case "/sc":
			if len(d.cfg.ScParams) == 0 {
				fmt.Println("No sc params set.")
			} else {
				fmt.Printf("SC: %s\n", d.cfg.ScParams)
			}
			continue
		case "/sc clear":
			d.cfg.ScParams = ""
			fmt.Println("SC params cleared.")
			continue
		case "/auth":
			d.handleAuth(ctx)
			continue
		case "/auth status":
			d.printAuthStatus()
			continue
		case "/auth logout":
			os.Remove(client.TokenStorePath())
			d.cfg.AuthToken = ""
			d.chatClient.SetAuthToken("")
			fmt.Println("Logged out. Token cleared.")
			continue
		}

		if strings.HasPrefix(input, "/model ") {
			d.handleModelCommand(strings.TrimPrefix(input, "/model "))
			continue
		}

		if strings.HasPrefix(input, "/sc ") {
			d.cfg.ScParams = strings.TrimPrefix(input, "/sc ")
			fmt.Printf("SC set to: %s\n", d.cfg.ScParams)
			continue
		}

		if input == "/mcp" || input == "/mcp list" {
			d.printMCPServers()
			continue
		}
		if strings.HasPrefix(input, "/mcp ") {
			d.handleMCPCommand(ctx, strings.TrimPrefix(input, "/mcp "))
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

func (d *Daemon) agentConfigWithModel() map[string]any {
	if len(d.modelSetID) == 0 {
		return nil
	}
	return map[string]any{"modelSetId": d.modelSetID}
}

func (d *Daemon) handleModelCommand(arg string) {
	arg = strings.TrimSpace(arg)

	if arg == "list" || arg == "ls" {
		d.printAvailableModels()
		return
	}

	models, err := d.fetchModels()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[error] failed to fetch models: %s\n", err)
		return
	}

	for _, m := range models {
		if strings.EqualFold(m.ID, arg) {
			d.modelSetID = m.ID
			displayName := m.ID
			if m.AgenticModel != nil && len(m.AgenticModel.DisplayName) > 0 {
				displayName = m.AgenticModel.DisplayName
			}
			fmt.Printf("Model set to: %s (%s)\n", m.ID, displayName)
			return
		}
	}

	fmt.Printf("Unknown model: %s\nUse /model list to see available models.\n", arg)
}

func (d *Daemon) printCurrentModel() {
	if len(d.modelSetID) == 0 {
		fmt.Println("No model set (using server default).")
		fmt.Println("Use /model list to see available models.")
		return
	}
	fmt.Printf("Current model: %s\n", d.modelSetID)
}

func (d *Daemon) printAvailableModels() {
	models, err := d.fetchModels()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[error] failed to fetch models: %s\n", err)
		return
	}
	if len(models) == 0 {
		fmt.Println("No models available.")
		return
	}

	fmt.Println("\nAvailable models:")
	for _, m := range models {
		marker := "  "
		if m.ID == d.modelSetID {
			marker = "▸ "
		}

		displayName := ""
		if m.AgenticModel != nil && len(m.AgenticModel.DisplayName) > 0 {
			displayName = m.AgenticModel.DisplayName
		}

		tags := ""
		if m.IsRecommended {
			tags += " [recommended]"
		}
		if m.IsPromoted {
			tags += " [promoted]"
		}

		if len(displayName) > 0 {
			fmt.Printf("%s%-28s %s%s\n", marker, m.ID, displayName, tags)
		} else {
			fmt.Printf("%s%s%s\n", marker, m.ID, tags)
		}
	}
	fmt.Println("\nUsage: /model <MODEL_ID>")
}

func (d *Daemon) fetchModels() ([]client.ModelSet, error) {
	if len(d.cachedModels) > 0 {
		return d.cachedModels, nil
	}
	models, err := d.getChatClient().FetchModels()
	if err != nil {
		return nil, err
	}
	d.cachedModels = models
	return models, nil
}

func (d *Daemon) sendChat(ctx context.Context, chatID, message string) (string, error) {
	clientTools := d.buildClientTools()

	agentCfg := d.agentConfigWithModel()
	req := client.ChatRequest{
		ChatID: chatID,
		Messages: []client.ChatMessage{
			{
				Author:      "USER",
				Fragments:   []client.ChatMessageFragment{{Text: message}},
				Platform:    "DESKTOP",
				AgentConfig: agentCfg,
			},
		},
		SaveChat:    true,
		ClientTools: clientTools,
		AgentConfig: agentCfg,
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

	spin := newSpinner()
	if round == 0 {
		spin.Start("thinking...")
	} else {
		spin.Start("processing tool results...")
	}

	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	cancelled := false
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		select {
		case <-sigCh:
			cancelled = true
			streamCancel()
		case <-streamCtx.Done():
		}
		signal.Stop(sigCh)
	}()

	respCh, errCh := d.getChatClient().StreamChatRequest(streamCtx, req)

	var lastChatID string
	var allText strings.Builder
	var toolRequests []client.ClientToolUseRequest
	var lastAgentConfig map[string]any
	gotFirstContent := false

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
					if !gotFirstContent {
						spin.Stop()
						gotFirstContent = true
					}
					fmt.Print(f.Text)
					allText.WriteString(f.Text)
				}
				if f.ToolUse != nil {
					if !gotFirstContent {
						spin.Stop()
						gotFirstContent = true
					}
					d.logger.Debug("received tool request", "tool_id", f.ToolUse.ToolID, "run_id", f.ToolUse.RunID)
					toolRequests = append(toolRequests, *f.ToolUse)
				}
			}
			if msg.AgentConfig != nil {
				lastAgentConfig = msg.AgentConfig
			}
		}
	}

	spin.Stop()
	signal.Stop(sigCh)

	if cancelled {
		fmt.Print("\n\033[2m[cancelled]\033[0m\n")
		if len(lastChatID) > 0 {
			go d.getChatClient().CancelChat(lastChatID)
		}
		<-errCh
		return lastChatID, nil
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
	clientTools := make([]client.ClientTool, 0, len(defs))
	for _, def := range defs {
		schema := toolSchemaToMap(def.InputSchema)
		// Ensure schema always has "type":"object" — backend requires it
		if schema["type"] == nil || schema["type"] == "" {
			schema["type"] = "object"
		}
		if schema["properties"] == nil {
			schema["properties"] = map[string]any{}
		}

		ct := client.ClientTool{
			ID:          def.ToolID,
			Name:        def.Name,
			Description: def.Description,
			InputSchema: schema,
		}

		if d.cfg.Debug {
			d.logger.Debug("registering client tool", "tool_id", def.ToolID, "name", def.Name)
		}

		clientTools = append(clientTools, ct)
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

func (d *Daemon) connectMCPAsync(ctx context.Context) {
	configs := d.MCP.Configs()
	enabled := 0
	for _, c := range configs {
		if c.Enabled {
			enabled++
		}
	}
	if enabled == 0 {
		return
	}

	mcpDone := make(chan struct{})
	go func() {
		defer close(mcpDone)
		for _, cfg := range configs {
			if !cfg.Enabled {
				continue
			}

			stopSpin := make(chan struct{})
			go func(name string) {
				i := 0
				for {
					select {
					case <-stopSpin:
						return
					default:
						fmt.Printf("\r\033[K\033[2m  %s connecting %s...\033[0m",
							spinnerFrames[i%len(spinnerFrames)], name)
						i++
						time.Sleep(80 * time.Millisecond)
					}
				}
			}(cfg.Name)

			_, err := d.MCP.Connect(ctx, cfg.Name)
			close(stopSpin)

			if err != nil {
				fmt.Printf("\r\033[K\033[31m  ✗ %s: %s\033[0m\n", cfg.Name, err)
				continue
			}
			d.MCP.RegisterMCPTools(d.registry)
			fmt.Printf("\r\033[K\033[32m  ✓ %s connected\033[0m\n", cfg.Name)
		}
		fmt.Printf("\033[2m  %d tools registered\033[0m", len(d.registry.Definitions()))
	}()

	time.Sleep(50 * time.Millisecond)
	<-mcpDone
}

func (d *Daemon) getChatClient() *client.ChatClient {
	return d.chatClient
}

func (d *Daemon) printBanner() {
	fmt.Println("╔══════════════════════════════════════════════╗")
	fmt.Println("║            gleand interactive mode           ║")
	fmt.Println("╠══════════════════════════════════════════════╣")
	fmt.Printf("║  Version:  %-33s║\n", d.Version)
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
	fmt.Println("  /tools         - List registered tools")
	fmt.Println("  /model         - Show current model")
	fmt.Println("  /model list    - List available models")
	fmt.Println("  /model <ID>    - Switch to a model")
	fmt.Println("  /new           - Start a new chat session")
	fmt.Println("  /id            - Show current chat ID")
	fmt.Println("  /sc            - Show current sc params")
	fmt.Println("  /sc <params>   - Set sc params")
	fmt.Println("  /sc clear      - Clear sc params")
	fmt.Println("  /auth          - Authenticate via browser OAuth")
	fmt.Println("  /auth status   - Show token status")
	fmt.Println("  /auth logout   - Clear saved token")
	fmt.Println("  /mcp           - List MCP servers")
	fmt.Println("  /mcp add <name> <cmd> [args] - Add stdio server")
	fmt.Println("  /mcp addurl <name> <url>     - Add HTTP server")
	fmt.Println("  /mcp enable|disable <name>   - Toggle server")
	fmt.Println("  /mcp connect|remove <name>   - Connect/remove")
	fmt.Println("  /debug [on|off]- Toggle debug logging")
	fmt.Println("  /quit          - Exit")
	fmt.Println()
	fmt.Println("Type any message to send to Glean Assistant.")
	fmt.Println("If the assistant wants to use a local tool, it will")
	fmt.Println("be executed automatically and results sent back.")
}

func (d *Daemon) printMCPServers() {
	configs := d.MCP.Configs()
	if len(configs) == 0 {
		fmt.Println("No MCP servers configured. Use /mcp add <name> <command> [args...]")
		return
	}

	fmt.Println("\nMCP servers:")
	for _, cfg := range configs {
		status := "\033[31m●\033[0m disabled"
		if cfg.Enabled {
			if d.MCP.IsConnected(cfg.Name) {
				status = "\033[32m●\033[0m connected"
			} else {
				status = "\033[33m●\033[0m disconnected"
			}
		}

		transport := cfg.Command
		if len(cfg.Args) > 0 {
			transport += " " + strings.Join(cfg.Args, " ")
		}
		if cfg.URL != "" {
			transport = cfg.URL
		}
		fmt.Printf("  %-16s %s  %s\n", cfg.Name, status, transport)
	}

	allTools := d.MCP.ConnectedTools()
	if len(allTools) > 0 {
		fmt.Println("\nMCP tools:")
		for server, serverTools := range allTools {
			for _, t := range serverTools {
				desc := t.Description
				if len(desc) > 50 {
					desc = desc[:50] + "..."
				}
				fmt.Printf("  [%s] %-24s %s\n", server, t.Name, desc)
			}
		}
	}
}

func (d *Daemon) handleMCPCommand(ctx context.Context, args string) {
	parts := strings.Fields(args)
	if len(parts) == 0 {
		d.printMCPServers()
		return
	}

	cmd := parts[0]
	switch cmd {
	case "add":
		if len(parts) < 3 {
			fmt.Println("Usage: /mcp add <name> <command> [args...]")
			return
		}
		name := parts[1]
		command := parts[2]
		cmdArgs := parts[3:]
		if err := d.MCP.Add(name, command, cmdArgs); err != nil {
			fmt.Fprintf(os.Stderr, "[error] %s\n", err)
			return
		}
		fmt.Printf("Added MCP server %q. Use /mcp connect %s to connect.\n", name, name)

	case "addurl":
		if len(parts) < 3 {
			fmt.Println("Usage: /mcp addurl <name> <url>")
			return
		}
		if err := d.MCP.AddURL(parts[1], parts[2]); err != nil {
			fmt.Fprintf(os.Stderr, "[error] %s\n", err)
			return
		}
		fmt.Printf("Added MCP server %q. Use /mcp connect %s to connect.\n", parts[1], parts[1])

	case "remove":
		if len(parts) < 2 {
			fmt.Println("Usage: /mcp remove <name>")
			return
		}
		if err := d.MCP.Remove(parts[1]); err != nil {
			fmt.Fprintf(os.Stderr, "[error] %s\n", err)
			return
		}
		fmt.Printf("Removed MCP server %q.\n", parts[1])

	case "enable":
		if len(parts) < 2 {
			fmt.Println("Usage: /mcp enable <name>")
			return
		}
		if err := d.MCP.Enable(parts[1]); err != nil {
			fmt.Fprintf(os.Stderr, "[error] %s\n", err)
			return
		}
		fmt.Printf("Enabled %q. Use /mcp connect %s to connect.\n", parts[1], parts[1])

	case "disable":
		if len(parts) < 2 {
			fmt.Println("Usage: /mcp disable <name>")
			return
		}
		if err := d.MCP.Disable(parts[1]); err != nil {
			fmt.Fprintf(os.Stderr, "[error] %s\n", err)
			return
		}
		fmt.Printf("Disabled %q.\n", parts[1])

	case "connect":
		if len(parts) < 2 {
			fmt.Println("Usage: /mcp connect <name>")
			return
		}
		name := parts[1]
		fmt.Printf("Connecting to %s...\n", name)
		mcpTools, err := d.MCP.Connect(ctx, name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[error] %s\n", err)
			return
		}
		d.MCP.RegisterMCPTools(d.registry)
		fmt.Printf("Connected. %d tools available:\n", len(mcpTools))
		for _, t := range mcpTools {
			fmt.Printf("  %-24s %s\n", t.Name, t.Description)
		}

	default:
		fmt.Printf("Unknown MCP command: %s\nUse /mcp for help.\n", cmd)
	}
}

func (d *Daemon) readMultiLineInput(line *liner.State) (string, error) {
	first, err := line.Prompt("gleand> ")
	if err != nil {
		return "", err
	}
	first = strings.TrimSpace(first)
	if !strings.HasSuffix(first, "\\") {
		return first, nil
	}

	var parts []string
	parts = append(parts, strings.TrimSuffix(first, "\\"))

	for {
		cont, err := line.Prompt("   ...> ")
		if err != nil {
			return strings.Join(parts, "\n"), nil
		}
		cont = strings.TrimSpace(cont)
		if strings.HasSuffix(cont, "\\") {
			parts = append(parts, strings.TrimSuffix(cont, "\\"))
			continue
		}
		parts = append(parts, cont)
		return strings.Join(parts, "\n"), nil
	}
}

func (d *Daemon) handleAuth(ctx context.Context) {
	oauth := client.NewOAuthClient(d.cfg.Backend, d.logger)
	token, err := oauth.Authorize(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[error] authentication failed: %s\n", err)
		return
	}

	if err := client.SaveToken(token); err != nil {
		fmt.Fprintf(os.Stderr, "[error] saving token: %s\n", err)
	}

	d.cfg.AuthToken = token.AccessToken
	d.chatClient.SetAuthToken(token.AccessToken)
	d.chatClient.SetUseRestAPI(true)
	fmt.Println("Authenticated successfully. Token saved.")
}

func (d *Daemon) printAuthStatus() {
	token, err := client.LoadToken()
	if err != nil {
		fmt.Println("No saved token.")
		return
	}
	expired := token.ExpiresAt > 0 && time.Now().Unix() > token.ExpiresAt
	status := "valid"
	if expired {
		status = "expired"
	}
	hasRefresh := "no"
	if len(token.RefreshToken) > 0 {
		hasRefresh = "yes"
	}
	fmt.Printf("Token file: %s\n", client.TokenStorePath())
	fmt.Printf("Token status: %s\n", status)
	fmt.Printf("Refresh token: %s\n", hasRefresh)
	if token.ExpiresAt > 0 {
		fmt.Printf("Expires: %s\n", time.Unix(token.ExpiresAt, 0).Format(time.RFC3339))
	}
}

func (d *Daemon) tryRestoreAuth() {
	if len(d.cfg.AuthToken) > 0 {
		return
	}

	token, err := client.LoadToken()
	if err != nil {
		return
	}

	expired := token.ExpiresAt > 0 && time.Now().Unix() > token.ExpiresAt
	if expired && len(token.RefreshToken) > 0 {
		oauth := client.NewOAuthClient(d.cfg.Backend, d.logger)
		refreshed, err := oauth.RefreshAccessToken(token.ClientID, token.RefreshToken)
		if err != nil {
			d.logger.Debug("auto-refresh failed", "error", err)
			return
		}
		client.SaveToken(refreshed)
		token = refreshed
	}

	if len(token.AccessToken) > 0 {
		d.cfg.AuthToken = token.AccessToken
		d.chatClient.SetAuthToken(token.AccessToken)
		d.chatClient.SetUseRestAPI(true)
	}
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
