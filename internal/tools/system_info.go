package tools

import (
	"os"
	"os/user"
	"runtime"
)

type SystemInfoTool struct{}

func NewSystemInfoTool() *SystemInfoTool {
	return &SystemInfoTool{}
}

func (t *SystemInfoTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "system_info",
		ToolID:      "desktop_system_info",
		Description: "Get system information about the user's local machine including OS, architecture, hostname, and environment.",
		InputSchema: ToolSchema{
			Type:       "object",
			Properties: map[string]Property{},
		},
	}
}

func (t *SystemInfoTool) Execute(params map[string]any) (*ToolResult, error) {
	hostname, _ := os.Hostname()
	homeDir, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()

	username := ""
	if u, err := user.Current(); err == nil {
		username = u.Username
	}

	return &ToolResult{Output: map[string]any{
		"os":          runtime.GOOS,
		"arch":        runtime.GOARCH,
		"hostname":    hostname,
		"username":    username,
		"home_dir":    homeDir,
		"working_dir": cwd,
		"num_cpu":     runtime.NumCPU(),
		"go_version":  runtime.Version(),
		"shell":       os.Getenv("SHELL"),
	}}, nil
}
