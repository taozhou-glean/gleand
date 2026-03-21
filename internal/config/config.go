package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/google/uuid"
)

const Version = "0.0.1"

// Config holds daemon configuration.
type Config struct {
	// Backend is the Glean backend URL (e.g., "https://scio-prod-be.glean.com").
	Backend string `json:"backend"`

	// AuthToken is the OAuth bearer token for API calls.
	// In standalone mode, this is set directly.
	// In Electron mode, it's received via IPC.
	AuthToken string `json:"authToken,omitempty"`

	// DeviceID uniquely identifies this machine. Generated on first run.
	DeviceID string `json:"deviceId"`

	// DeviceName is a human-readable name for this device.
	DeviceName string `json:"deviceName"`

	// PollIntervalSeconds is the interval between chat polling cycles.
	PollIntervalSeconds int `json:"pollIntervalSeconds"`

	// HeartbeatIntervalSeconds is the interval between device heartbeats.
	HeartbeatIntervalSeconds int `json:"heartbeatIntervalSeconds"`

	// MaxCommandTimeout is the maximum allowed timeout for command execution.
	MaxCommandTimeout int `json:"maxCommandTimeout"`

	// AllowedPaths are the paths that file operations are sandboxed to.
	AllowedPaths []string `json:"allowedPaths"`

	// BlockedCommands are command prefixes that are never allowed.
	BlockedCommands []string `json:"blockedCommands"`

	// AutoApprove when true, tools execute without user confirmation.
	AutoApprove bool `json:"autoApprove"`

	// LogFile is the path to the log file. Empty means stdout.
	LogFile string `json:"logFile,omitempty"`

	// ScParams are additional sc params passed to the chat API.
	ScParams string `json:"scParams,omitempty"`

	// Debug enables verbose logging with colored output.
	Debug bool `json:"-"`
}

// DefaultConfig returns a config with sensible defaults.
func DefaultConfig() *Config {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}

	homeDir, _ := os.UserHomeDir()
	if homeDir == "" {
		homeDir = "/"
	}

	return &Config{
		Backend:                  "https://scio-prod-be.glean.com",
		DeviceID:                 uuid.New().String(),
		DeviceName:               fmt.Sprintf("%s (%s/%s)", hostname, runtime.GOOS, runtime.GOARCH),
		PollIntervalSeconds:      5,
		HeartbeatIntervalSeconds: 30,
		MaxCommandTimeout:        300,
		AllowedPaths:             []string{homeDir},
		BlockedCommands: []string{
			"sudo",
			"rm -rf /",
			"mkfs",
			"dd if=",
			":(){ :|:& };:",
		},
		AutoApprove: false,
	}
}

// ConfigPath returns the path to the config file.
func ConfigPath() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(configDir, "gleand", "config.json")
}

// Load reads config from disk, returning defaults if the file doesn't exist.
func Load() (*Config, error) {
	cfg := DefaultConfig()
	path := ConfigPath()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Save defaults for next time
			_ = cfg.Save()
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	return cfg, nil
}

// Save writes the config to disk.
func (c *Config) Save() error {
	path := ConfigPath()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	return os.WriteFile(path, data, 0o600)
}
