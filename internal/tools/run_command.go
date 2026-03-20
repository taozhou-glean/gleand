package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type RunCommandTool struct {
	maxTimeout      int
	blockedCommands []string
}

func NewRunCommandTool(maxTimeout int, blockedCommands []string) *RunCommandTool {
	return &RunCommandTool{
		maxTimeout:      maxTimeout,
		blockedCommands: blockedCommands,
	}
}

func (t *RunCommandTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "run_command",
		ToolID:      "desktop_run_command",
		Description: "Execute a shell command on the user's local machine and return stdout, stderr, and exit code.",
		InputSchema: ToolSchema{
			Type: "object",
			Properties: map[string]Property{
				"command": {
					Type:        "string",
					Description: "Shell command to execute",
				},
				"working_directory": {
					Type:        "string",
					Description: "Working directory for the command. Defaults to user home directory.",
				},
				"timeout_seconds": {
					Type:        "integer",
					Description: "Timeout in seconds. Default 30, max 300.",
					Default:     30,
				},
			},
			Required: []string{"command"},
		},
	}
}

func (t *RunCommandTool) Execute(params map[string]any) (*ToolResult, error) {
	command, ok := params["command"].(string)
	if !ok || command == "" {
		return &ToolResult{
			Output: map[string]any{"error": "command parameter is required"},
		}, nil
	}

	for _, blocked := range t.blockedCommands {
		if strings.Contains(command, blocked) {
			return &ToolResult{
				Output: map[string]any{"error": fmt.Sprintf("command contains blocked pattern: %s", blocked)},
			}, nil
		}
	}

	timeoutSec := 30
	if ts, ok := params["timeout_seconds"].(float64); ok {
		timeoutSec = int(ts)
	}
	if timeoutSec > t.maxTimeout {
		timeoutSec = t.maxTimeout
	}
	if timeoutSec <= 0 {
		timeoutSec = 30
	}

	workDir, _ := params["working_directory"].(string)
	if workDir == "" {
		workDir, _ = os.Getwd()
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}
		cmd = exec.CommandContext(ctx, shell, "-c", command)
	}

	cmd.Dir = workDir
	cmd.Env = os.Environ()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	exitCode := 0
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return &ToolResult{
				Output: map[string]any{
					"error":       "command timed out",
					"timeout_sec": timeoutSec,
					"stdout":      truncate(stdout.String(), 10000),
					"stderr":      truncate(stderr.String(), 10000),
				},
			}, nil
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return &ToolResult{
				Output: map[string]any{"error": fmt.Sprintf("failed to start command: %s", err)},
			}, nil
		}
	}

	return &ToolResult{
		Output: map[string]any{
			"stdout":      truncate(stdout.String(), 50000),
			"stderr":      truncate(stderr.String(), 10000),
			"exit_code":   exitCode,
			"duration_ms": duration.Milliseconds(),
		},
	}, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "\n... [truncated]"
}
