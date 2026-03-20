package tools

import (
	"testing"
)

type mockTool struct {
	def ToolDefinition
}

func (m *mockTool) Definition() ToolDefinition { return m.def }
func (m *mockTool) Execute(params map[string]any) (*ToolResult, error) {
	return &ToolResult{Output: map[string]any{"echo": params["input"]}}, nil
}

func TestRegistry(t *testing.T) {
	r := NewRegistry()

	tool := &mockTool{def: ToolDefinition{
		Name:   "test_tool",
		ToolID: "test_id",
	}}

	r.Register(tool)

	if _, ok := r.Get("test_id"); !ok {
		t.Fatal("expected to find registered tool")
	}

	if _, ok := r.Get("nonexistent"); ok {
		t.Fatal("expected not to find unregistered tool")
	}

	defs := r.Definitions()
	if len(defs) != 1 {
		t.Fatalf("expected 1 definition, got %d", len(defs))
	}
	if defs[0].ToolID != "test_id" {
		t.Fatalf("expected tool_id 'test_id', got '%s'", defs[0].ToolID)
	}

	result, err := r.Execute("test_id", map[string]any{"input": "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output["echo"] != "hello" {
		t.Fatalf("expected echo 'hello', got '%v'", result.Output["echo"])
	}

	_, err = r.Execute("nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}
