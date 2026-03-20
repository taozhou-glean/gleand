package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadFileTool(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewReadFileTool([]string{tmpDir})

	testFile := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(testFile, []byte("hello world"), 0o644)

	t.Run("read allowed file", func(t *testing.T) {
		result, err := tool.Execute(map[string]any{"path": testFile})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Output["content"] != "hello world" {
			t.Fatalf("expected 'hello world', got %v", result.Output["content"])
		}
	})

	t.Run("read outside sandbox", func(t *testing.T) {
		result, err := tool.Execute(map[string]any{"path": "/etc/hosts"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := result.Output["error"]; !ok {
			t.Fatal("expected error for path outside sandbox")
		}
	})

	t.Run("read nonexistent file", func(t *testing.T) {
		result, err := tool.Execute(map[string]any{"path": filepath.Join(tmpDir, "nope.txt")})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := result.Output["error"]; !ok {
			t.Fatal("expected error for nonexistent file")
		}
	})

	t.Run("truncation", func(t *testing.T) {
		result, err := tool.Execute(map[string]any{"path": testFile, "max_bytes": float64(5)})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Output["content"] != "hello" {
			t.Fatalf("expected 'hello', got %v", result.Output["content"])
		}
		if result.Output["truncated"] != true {
			t.Fatal("expected truncated=true")
		}
	})
}
