package client

// REST API types matching the Glean chat protocol.
// These mirror the Go types in go/query_endpoint/rest/types/.

type ChatRequest struct {
	ChatID      string         `json:"chatId,omitempty"`
	Messages    []ChatMessage  `json:"messages,omitempty"`
	MessageType string         `json:"messageType,omitempty"`
	AgentConfig map[string]any `json:"agentConfig,omitempty"`
	Stream      bool           `json:"stream,omitempty"`
	SaveChat    bool           `json:"saveChat,omitempty"`
	ClientTools []ClientTool   `json:"clientTools,omitempty"`
	Sc          string         `json:"sc,omitempty"`
}

type ClientTool struct {
	ID          string         `json:"id,omitempty"`
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

type ChatMessage struct {
	ID          string                `json:"id,omitempty"`
	Author      string                `json:"author,omitempty"`
	Fragments   []ChatMessageFragment `json:"fragments,omitempty"`
	MessageType string                `json:"messageType,omitempty"`
	Platform    string                `json:"platform,omitempty"`
	AgentConfig map[string]any        `json:"agentConfig,omitempty"`
}

type ChatMessageFragment struct {
	Text              string                `json:"text,omitempty"`
	ToolUse           *ClientToolUseRequest `json:"toolUse,omitempty"`
	ToolUseResult     *ClientToolUseResult  `json:"toolUseResult,omitempty"`
	ServerToolRequest *ServerToolRequest    `json:"serverToolRequest,omitempty"`
}

type ClientToolUseRequest struct {
	RunID      string         `json:"runId"`
	ToolID     string         `json:"toolId"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

type ClientToolUseResult struct {
	RunID  string         `json:"runId"`
	ToolID string         `json:"toolId"`
	Output map[string]any `json:"output"`
}

type ServerToolRequest struct {
	ToolType        string `json:"toolType,omitempty"`
	RequestType     string `json:"requestType,omitempty"`
	RequestID       string `json:"requestId,omitempty"`
	ToolName        string `json:"toolName,omitempty"`
	ToolDisplayName string `json:"toolDisplayName,omitempty"`
	ToolDescription string `json:"toolDescription,omitempty"`
}

type ChatResponse struct {
	ChatID   string        `json:"chatId,omitempty"`
	Messages []ChatMessage `json:"messages,omitempty"`
}

type ToolRegistration struct {
	DeviceID   string           `json:"deviceId"`
	DeviceName string           `json:"deviceName"`
	Tools      []ToolDefinition `json:"tools"`
}

type ToolDefinition struct {
	Name        string         `json:"name"`
	ToolID      string         `json:"toolId"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type DeviceHeartbeat struct {
	DeviceID   string `json:"deviceId"`
	DeviceName string `json:"deviceName"`
	Status     string `json:"status"`
}

type ConfigResponse struct {
	ClientConfig *ClientConfigData `json:"clientConfig,omitempty"`
}

type ClientConfigData struct {
	Assistant *AssistantConfig `json:"assistant,omitempty"`
}

type AssistantConfig struct {
	AvailableModelSets []ModelSet `json:"availableModelSets,omitempty"`
}

type ModelSet struct {
	ID               string     `json:"id,omitempty"`
	AgenticModel     *ModelInfo `json:"agenticModel,omitempty"`
	FastAgenticModel *ModelInfo `json:"fastAgenticModel,omitempty"`
	IsRecommended    bool       `json:"isRecommended,omitempty"`
	IsPromoted       bool       `json:"isPromoted,omitempty"`
}

type ModelInfo struct {
	DisplayName string `json:"displayName,omitempty"`
	Provider    string `json:"provider,omitempty"`
}
