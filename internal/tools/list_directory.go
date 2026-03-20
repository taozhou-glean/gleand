package tools

import (
	"fmt"
	"os"
	"path/filepath"
)

type ListDirectoryTool struct {
	allowedPaths []string
}

func NewListDirectoryTool(allowedPaths []string) *ListDirectoryTool {
	return &ListDirectoryTool{allowedPaths: allowedPaths}
}

func (t *ListDirectoryTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "list_directory",
		ToolID:      "desktop_list_directory",
		Description: "List the contents of a directory on the user's local machine.",
		InputSchema: ToolSchema{
			Type: "object",
			Properties: map[string]Property{
				"path": {
					Type:        "string",
					Description: "Absolute path to the directory to list",
				},
				"max_entries": {
					Type:        "integer",
					Description: "Maximum number of entries to return. Default 200.",
					Default:     200,
				},
			},
			Required: []string{"path"},
		},
	}
}

func (t *ListDirectoryTool) Execute(params map[string]any) (*ToolResult, error) {
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

	maxEntries := 200
	if me, ok := params["max_entries"].(float64); ok && me > 0 {
		maxEntries = int(me)
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return &ToolResult{Output: map[string]any{"error": fmt.Sprintf("cannot read directory: %s", err)}}, nil
	}

	type entry struct {
		Name string `json:"name"`
		Type string `json:"type"`
		Size int64  `json:"size"`
	}

	items := make([]entry, 0, min(len(entries), maxEntries))
	for i, e := range entries {
		if i >= maxEntries {
			break
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		entryType := "file"
		if e.IsDir() {
			entryType = "directory"
		} else if info.Mode()&os.ModeSymlink != 0 {
			entryType = "symlink"
		}
		items = append(items, entry{
			Name: e.Name(),
			Type: entryType,
			Size: info.Size(),
		})
	}

	return &ToolResult{Output: map[string]any{
		"path":      absPath,
		"entries":   items,
		"count":     len(items),
		"total":     len(entries),
		"truncated": len(entries) > maxEntries,
	}}, nil
}
