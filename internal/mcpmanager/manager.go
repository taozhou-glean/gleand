package mcpmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/taozhou/gleand/internal/config"
	"github.com/taozhou/gleand/internal/tools"
)

type ServerConfig struct {
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	URL     string   `json:"url,omitempty"`
	Enabled bool     `json:"enabled"`
}

type connectedServer struct {
	config  ServerConfig
	session *mcp.ClientSession
	tools   []*mcp.Tool
}

type Manager struct {
	mu      sync.Mutex
	servers map[string]*connectedServer
	configs []ServerConfig
}

func New() *Manager {
	return &Manager{
		servers: make(map[string]*connectedServer),
	}
}

func configPath() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(configDir, "gleand", "mcp.json")
}

func (m *Manager) LoadConfigs() error {
	data, err := os.ReadFile(configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return json.Unmarshal(data, &m.configs)
}

func (m *Manager) SaveConfigs() error {
	path := configPath()
	os.MkdirAll(filepath.Dir(path), 0o755)
	data, err := json.MarshalIndent(m.configs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (m *Manager) Add(name, command string, args []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, c := range m.configs {
		if c.Name == name {
			return fmt.Errorf("server %q already exists", name)
		}
	}

	cfg := ServerConfig{
		Name:    name,
		Command: command,
		Args:    args,
		Enabled: true,
	}
	m.configs = append(m.configs, cfg)
	return m.SaveConfigs()
}

func (m *Manager) AddURL(name, url string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, c := range m.configs {
		if c.Name == name {
			return fmt.Errorf("server %q already exists", name)
		}
	}

	cfg := ServerConfig{
		Name:    name,
		URL:     url,
		Enabled: true,
	}
	m.configs = append(m.configs, cfg)
	return m.SaveConfigs()
}

func (m *Manager) Remove(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.disconnectLocked(name)

	filtered := m.configs[:0]
	found := false
	for _, c := range m.configs {
		if c.Name == name {
			found = true
			continue
		}
		filtered = append(filtered, c)
	}
	if !found {
		return fmt.Errorf("server %q not found", name)
	}
	m.configs = filtered
	return m.SaveConfigs()
}

func (m *Manager) Enable(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.setEnabled(name, true)
}

func (m *Manager) Disable(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.disconnectLocked(name)
	return m.setEnabled(name, false)
}

func (m *Manager) setEnabled(name string, enabled bool) error {
	for i, c := range m.configs {
		if c.Name == name {
			m.configs[i].Enabled = enabled
			return m.SaveConfigs()
		}
	}
	return fmt.Errorf("server %q not found", name)
}

func (m *Manager) Connect(ctx context.Context, name string) ([]*mcp.Tool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if srv, ok := m.servers[name]; ok {
		return srv.tools, nil
	}

	var cfg *ServerConfig
	for _, c := range m.configs {
		if c.Name == name {
			c := c
			cfg = &c
			break
		}
	}
	if cfg == nil {
		return nil, fmt.Errorf("server %q not found", name)
	}
	if !cfg.Enabled {
		return nil, fmt.Errorf("server %q is disabled", name)
	}

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "gleand",
		Version: config.Version,
	}, nil)

	var transport mcp.Transport
	if cfg.URL != "" {
		transport = &mcp.StreamableClientTransport{Endpoint: cfg.URL}
	} else {
		cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
		transport = &mcp.CommandTransport{Command: cmd}
	}

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", name, err)
	}

	result, err := session.ListTools(ctx, nil)
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("listing tools from %s: %w", name, err)
	}

	m.servers[name] = &connectedServer{
		config:  *cfg,
		session: session,
		tools:   result.Tools,
	}

	return result.Tools, nil
}

func (m *Manager) ConnectAll(ctx context.Context) map[string]error {
	errors := make(map[string]error)
	for _, cfg := range m.configs {
		if !cfg.Enabled {
			continue
		}
		if _, err := m.Connect(ctx, cfg.Name); err != nil {
			errors[cfg.Name] = err
		}
	}
	return errors
}

func (m *Manager) Disconnect(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.disconnectLocked(name)
}

func (m *Manager) disconnectLocked(name string) {
	if srv, ok := m.servers[name]; ok {
		srv.session.Close()
		delete(m.servers, name)
	}
}

func (m *Manager) DisconnectAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, srv := range m.servers {
		srv.session.Close()
		delete(m.servers, name)
	}
}

func (m *Manager) CallTool(ctx context.Context, serverName, toolName string, args map[string]any) (*mcp.CallToolResult, error) {
	m.mu.Lock()
	srv, ok := m.servers[serverName]
	m.mu.Unlock()

	if !ok {
		return nil, fmt.Errorf("server %q is not connected", serverName)
	}

	return srv.session.CallTool(ctx, &mcp.CallToolParams{
		Name:      toolName,
		Arguments: args,
	})
}

func (m *Manager) Configs() []ServerConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ServerConfig, len(m.configs))
	copy(out, m.configs)
	return out
}

func (m *Manager) IsConnected(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.servers[name]
	return ok
}

func (m *Manager) ConnectedTools() map[string][]*mcp.Tool {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[string][]*mcp.Tool)
	for name, srv := range m.servers {
		result[name] = srv.tools
	}
	return result
}

func (m *Manager) RegisterMCPTools(registry *tools.Registry) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for serverName, srv := range m.servers {
		for _, t := range srv.tools {
			registry.Register(newMCPToolAdapter(serverName, t, m))
		}
	}
}

type mcpToolAdapter struct {
	serverName string
	tool       *mcp.Tool
	manager    *Manager
}

func newMCPToolAdapter(serverName string, tool *mcp.Tool, manager *Manager) *mcpToolAdapter {
	return &mcpToolAdapter{serverName: serverName, tool: tool, manager: manager}
}

func (a *mcpToolAdapter) Definition() tools.ToolDefinition {
	toolID := fmt.Sprintf("mcp_%s_%s", a.serverName, a.tool.Name)
	toolID = strings.ReplaceAll(toolID, "-", "_")

	schema := tools.ToolSchema{
		Type:       "object",
		Properties: make(map[string]tools.Property),
	}

	if a.tool.InputSchema != nil {
		schemaBytes, _ := json.Marshal(a.tool.InputSchema)
		var schemaMap map[string]any
		json.Unmarshal(schemaBytes, &schemaMap)

		if props, ok := schemaMap["properties"].(map[string]any); ok {
			for name, prop := range props {
				propMap, ok := prop.(map[string]any)
				if !ok {
					continue
				}
				p := tools.Property{}
				switch t := propMap["type"].(type) {
				case string:
					p.Type = t
				case []any:
					for _, v := range t {
						if s, ok := v.(string); ok && s != "null" {
							p.Type = s
							break
						}
					}
				}
				if p.Type == "" {
					p.Type = "string"
				}
				if d, ok := propMap["description"].(string); ok {
					p.Description = d
				}
				schema.Properties[name] = p
			}
		}
		if req, ok := schemaMap["required"].([]any); ok {
			for _, r := range req {
				if s, ok := r.(string); ok {
					schema.Required = append(schema.Required, s)
				}
			}
		}
	}

	desc := a.tool.Description
	if desc == "" {
		desc = fmt.Sprintf("MCP tool from %s", a.serverName)
	}

	return tools.ToolDefinition{
		Name:        a.tool.Name,
		ToolID:      toolID,
		Description: fmt.Sprintf("[%s] %s", a.serverName, desc),
		InputSchema: schema,
	}
}

func (a *mcpToolAdapter) Execute(params map[string]any) (*tools.ToolResult, error) {
	result, err := a.manager.CallTool(context.Background(), a.serverName, a.tool.Name, params)
	if err != nil {
		return &tools.ToolResult{Output: map[string]any{"error": err.Error()}}, nil
	}

	output := make(map[string]any)
	if result.IsError {
		output["error"] = "tool returned an error"
	}
	var textParts []string
	for _, c := range result.Content {
		contentBytes, _ := json.Marshal(c)
		var contentMap map[string]any
		json.Unmarshal(contentBytes, &contentMap)
		if contentMap["type"] == "text" {
			if text, ok := contentMap["text"].(string); ok {
				textParts = append(textParts, text)
			}
		}
	}
	if len(textParts) > 0 {
		output["result"] = strings.Join(textParts, "\n")
	}

	return &tools.ToolResult{Output: output}, nil
}
