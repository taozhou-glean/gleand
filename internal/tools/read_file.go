package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ReadFileTool struct {
	allowedPaths []string
}

func NewReadFileTool(allowedPaths []string) *ReadFileTool {
	return &ReadFileTool{allowedPaths: allowedPaths}
}

func (t *ReadFileTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "read_file",
		ToolID:      "desktop_read_file",
		Description: "Read the contents of a file on the user's local machine.",
		InputSchema: ToolSchema{
			Type: "object",
			Properties: map[string]Property{
				"path": {
					Type:        "string",
					Description: "Absolute path to the file to read",
				},
				"max_bytes": {
					Type:        "integer",
					Description: "Maximum number of bytes to read. Default 100000.",
					Default:     100000,
				},
			},
			Required: []string{"path"},
		},
	}
}

func (t *ReadFileTool) Execute(params map[string]any) (*ToolResult, error) {
	path, ok := params["path"].(string)
	if !ok || path == "" {
		return &ToolResult{Output: map[string]any{"error": "path parameter is required"}}, nil
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

	maxBytes := 100000
	if mb, ok := params["max_bytes"].(float64); ok && mb > 0 {
		maxBytes = int(mb)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return &ToolResult{Output: map[string]any{"error": fmt.Sprintf("cannot stat file: %s", err)}}, nil
	}
	if info.IsDir() {
		return &ToolResult{Output: map[string]any{"error": "path is a directory, use desktop_list_directory instead"}}, nil
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return &ToolResult{Output: map[string]any{"error": fmt.Sprintf("cannot read file: %s", err)}}, nil
	}

	content := string(data)
	truncated := false
	if len(content) > maxBytes {
		content = content[:maxBytes]
		truncated = true
	}

	return &ToolResult{Output: map[string]any{
		"content":    content,
		"size_bytes": info.Size(),
		"truncated":  truncated,
	}}, nil
}

func isPathAllowed(absPath string, allowedPaths []string) bool {
	if len(allowedPaths) == 0 {
		return true
	}
	for _, allowed := range allowedPaths {
		allowedAbs, err := filepath.Abs(allowed)
		if err != nil {
			continue
		}
		if strings.HasPrefix(absPath, allowedAbs+string(filepath.Separator)) || absPath == allowedAbs {
			return true
		}
	}
	return false
}
