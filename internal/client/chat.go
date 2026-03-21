package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

type ChatClient struct {
	baseURL    string
	authToken  string
	httpClient *http.Client
	logger     *slog.Logger
	useRestAPI bool
}

func NewChatClient(baseURL, authToken string, logger *slog.Logger) *ChatClient {
	return &ChatClient{
		baseURL:    baseURL,
		authToken:  authToken,
		httpClient: &http.Client{},
		logger:     logger,
	}
}

func (c *ChatClient) SetAuthToken(token string) {
	c.authToken = token
}

func (c *ChatClient) SetUseRestAPI(use bool) {
	c.useRestAPI = use
}

func (c *ChatClient) chatPath() string {
	if c.useRestAPI {
		return "/rest/api/v1/chat"
	}
	return "/api/v1/chat"
}

func (c *ChatClient) SendMessage(chatID string, fragments []ChatMessageFragment, agentConfig map[string]any) (*ChatResponse, error) {
	req := ChatRequest{
		ChatID: chatID,
		Messages: []ChatMessage{
			{
				Author:      "USER",
				Fragments:   fragments,
				MessageType: "TOOL_USE",
				Platform:    "DESKTOP",
			},
		},
		MessageType: "TOOL_USE",
		AgentConfig: agentConfig,
		Stream:      false,
	}

	return c.sendChatRequest(req)
}

func (c *ChatClient) SendToolResults(chatID string, results []ClientToolUseResult, agentConfig map[string]any) (*ChatResponse, error) {
	fragments := make([]ChatMessageFragment, len(results))
	for i, r := range results {
		r := r
		fragments[i] = ChatMessageFragment{
			ToolUseResult: &r,
		}
	}
	return c.SendMessage(chatID, fragments, agentConfig)
}

func (c *ChatClient) StreamChatRequest(ctx context.Context, req ChatRequest) (<-chan ChatResponse, <-chan error) {
	responseCh := make(chan ChatResponse, 10)
	errCh := make(chan error, 1)

	go func() {
		defer close(responseCh)
		defer close(errCh)

		req.Stream = true

		body, err := json.Marshal(req)
		if err != nil {
			errCh <- fmt.Errorf("marshaling request: %w", err)
			return
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+c.chatPath(), bytes.NewReader(body))
		if err != nil {
			errCh <- fmt.Errorf("creating request: %w", err)
			return
		}
		c.setHeaders(httpReq)

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			errCh <- fmt.Errorf("sending request: %w", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			errCh <- fmt.Errorf("chat API returned %d: %s", resp.StatusCode, string(bodyBytes))
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

		for scanner.Scan() {
			if ctx.Err() != nil {
				return
			}
			line := scanner.Text()
			if line == "" {
				continue
			}

			var chatResp ChatResponse
			if err := json.Unmarshal([]byte(line), &chatResp); err != nil {
				c.logger.Debug("skipping non-JSON line in stream", "line", line)
				continue
			}
			responseCh <- chatResp
		}

		if err := scanner.Err(); err != nil && ctx.Err() == nil {
			errCh <- fmt.Errorf("reading stream: %w", err)
		}
	}()

	return responseCh, errCh
}

func (c *ChatClient) CancelChat(chatID string) error {
	httpReq, err := http.NewRequest("POST", c.baseURL+c.chatPath()+"/"+chatID+"/cancel", bytes.NewReader([]byte("{}")))
	if err != nil {
		return fmt.Errorf("creating cancel request: %w", err)
	}
	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("sending cancel request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("cancel API returned %d: %s", resp.StatusCode, string(bodyBytes))
	}
	return nil
}

func (c *ChatClient) sendChatRequest(req ChatRequest) (*ChatResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", c.baseURL+c.chatPath(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("chat API returned %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &chatResp, nil
}

func (c *ChatClient) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.authToken)
	req.Header.Set("X-Glean-Client", "gleand/0.1.0")
}

func (c *ChatClient) FetchModels() ([]ModelSet, error) {
	httpReq, err := http.NewRequest("POST", c.baseURL+"/api/v1/config", bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("fetching config: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("config API returned %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var configResp ConfigResponse
	if err := json.NewDecoder(resp.Body).Decode(&configResp); err != nil {
		return nil, fmt.Errorf("decoding config: %w", err)
	}

	if configResp.ClientConfig == nil || configResp.ClientConfig.Assistant == nil {
		return nil, nil
	}
	return configResp.ClientConfig.Assistant.AvailableModelSets, nil
}

func ExtractToolRequests(resp *ChatResponse) []ClientToolUseRequest {
	var requests []ClientToolUseRequest
	for _, msg := range resp.Messages {
		for _, f := range msg.Fragments {
			if f.ToolUse != nil {
				requests = append(requests, *f.ToolUse)
			}
		}
	}
	return requests
}
