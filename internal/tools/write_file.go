package tools

import (
	"fmt"
	"os"
	"path/filepath"
)

type WriteFileTool struct {
	allowedPaths []string
}

func NewWriteFileTool(allowedPaths []string) *WriteFileTool {
	return &WriteFileTool{allowedPaths: allowedPaths}
}

func (t *WriteFileTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "write_file",
		ToolID:      "desktop_write_file",
		Description: "Write content to a file on the user's local machine. Creates the file if it doesn't exist, overwrites if it does.",
		InputSchema: ToolSchema{
			Type: "object",
			Properties: map[string]Property{
				"path": {
					Type:        "string",
					Description: "Absolute path to the file to write",
				},
				"content": {
					Type:        "string",
					Description: "Content to write to the file",
				},
				"append": {
					Type:        "boolean",
					Description: "If true, append to the file instead of overwriting. Default false.",
					Default:     false,
				},
			},
			Required: []string{"path", "content"},
		},
	}
}

func (t *WriteFileTool) Execute(params map[string]any) (*ToolResult, error) {
	path, ok := params["path"].(string)
	if !ok || path == "" {
		return &ToolResult{Output: map[string]any{"error": "path parameter is required"}}, nil
	}

	content, ok := params["content"].(string)
	if !ok {
		return &ToolResult{Output: map[string]any{"error": "content parameter is required"}}, nil
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return &ToolResult{Output: map[string]any{"error": fmt.Sprintf("invalid path: %s", err)}}, nil
	}

	if !isPathAllowed(absPath, t.allowedPaths) {
		return &ToolResult{Output: map[string]any{
			"error": fmt.Sprintf("path %s is outside allowed directories", absPath),
		}}, nil
	}

	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return &ToolResult{Output: map[string]any{"error": fmt.Sprintf("cannot create directory: %s", err)}}, nil
	}

	appendMode, _ := params["append"].(bool)

	var flag int
	if appendMode {
		flag = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	} else {
		flag = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}

	f, err := os.OpenFile(absPath, flag, 0o644)
	if err != nil {
		return &ToolResult{Output: map[string]any{"error": fmt.Sprintf("cannot open file: %s", err)}}, nil
	}
	defer f.Close()

	n, err := f.WriteString(content)
	if err != nil {
		return &ToolResult{Output: map[string]any{"error": fmt.Sprintf("write failed: %s", err)}}, nil
	}

	return &ToolResult{Output: map[string]any{
		"bytes_written": n,
		"path":          absPath,
		"mode":          map[bool]string{true: "append", false: "overwrite"}[appendMode],
	}}, nil
}
