package tools

import (
	"encoding/json"
	"fmt"
	"sync"
)

type ToolSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties"`
	Required   []string            `json:"required,omitempty"`
}

type Property struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Default     any    `json:"default,omitempty"`
}

type ToolDefinition struct {
	Name           string         `json:"name"`
	ToolID         string         `json:"toolId"`
	Description    string         `json:"description"`
	InputSchema    ToolSchema     `json:"input_schema"`
	RawInputSchema map[string]any `json:"raw_input_schema,omitempty"`
}

type ToolResult struct {
	Output map[string]any `json:"output"`
	Error  string         `json:"error,omitempty"`
}

type Tool interface {
	Definition() ToolDefinition
	Execute(params map[string]any) (*ToolResult, error)
}

type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

func (r *Registry) Register(tool Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	def := tool.Definition()
	r.tools[def.ToolID] = tool
}

func (r *Registry) Get(toolID string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[toolID]
	return t, ok
}

func (r *Registry) Definitions() []ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]ToolDefinition, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, t.Definition())
	}
	return defs
}

func (r *Registry) Execute(toolID string, params map[string]any) (*ToolResult, error) {
	tool, ok := r.Get(toolID)
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", toolID)
	}
	return tool.Execute(params)
}

func (r *Registry) DefinitionsJSON() ([]byte, error) {
	return json.Marshal(r.Definitions())
}
