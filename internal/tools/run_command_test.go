package tools

import (
	"runtime"
	"testing"
)

func TestRunCommandTool(t *testing.T) {
	tool := NewRunCommandTool(30, []string{"sudo", "rm -rf /"})

	t.Run("simple echo", func(t *testing.T) {
		result, err := tool.Execute(map[string]any{"command": "echo hello"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		stdout := result.Output["stdout"].(string)
		if stdout != "hello\n" {
			t.Fatalf("expected 'hello\\n', got %q", stdout)
		}
		if result.Output["exit_code"].(int) != 0 {
			t.Fatalf("expected exit code 0, got %v", result.Output["exit_code"])
		}
	})

	t.Run("blocked command", func(t *testing.T) {
		result, err := tool.Execute(map[string]any{"command": "sudo apt install something"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		errMsg, ok := result.Output["error"].(string)
		if !ok || errMsg == "" {
			t.Fatal("expected error in output for blocked command")
		}
	})

	t.Run("missing command parameter", func(t *testing.T) {
		result, err := tool.Execute(map[string]any{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := result.Output["error"]; !ok {
			t.Fatal("expected error for missing command")
		}
	})

	t.Run("nonzero exit code", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("skipping on windows")
		}
		result, err := tool.Execute(map[string]any{"command": "exit 42"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Output["exit_code"].(int) != 42 {
			t.Fatalf("expected exit code 42, got %v", result.Output["exit_code"])
		}
	})

	t.Run("timeout", func(t *testing.T) {
		result, err := tool.Execute(map[string]any{
			"command":         "sleep 10",
			"timeout_seconds": float64(1),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := result.Output["error"]; !ok {
			t.Fatal("expected timeout error")
		}
	})
}
