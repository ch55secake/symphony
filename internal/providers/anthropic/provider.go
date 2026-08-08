// Package anthropic implements the Anthropic Messages API for agent turns.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ch55secake/symphony/internal/agent"
	"github.com/ch55secake/symphony/internal/events"
	"github.com/ch55secake/symphony/internal/providers"
)

const (
	defaultBaseURL   = "https://api.anthropic.com"
	defaultMaxTokens = 4096
	apiVersion       = "2023-06-01"
)

// Config supplies connection details. APIKey is never persisted by this package.
type Config struct {
	APIKey     string
	BaseURL    string
	MaxTokens  int
	HTTPClient *http.Client
}

// Provider completes agent turns through the Anthropic Messages API.
type Provider struct {
	apiKey     string
	baseURL    string
	maxTokens  int
	httpClient *http.Client
}

// Error is a safe representation of a non-successful Anthropic response.
type Error struct {
	StatusCode int
	Detail     string
}

func (e *Error) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("Anthropic response returned HTTP %d: %s", e.StatusCode, e.Detail)
	}
	return fmt.Sprintf("Anthropic response returned HTTP %d", e.StatusCode)
}

func New(config Config) (*Provider, error) {
	if strings.TrimSpace(config.APIKey) == "" {
		return nil, errors.New("Anthropic API key is required")
	}
	baseURL := strings.TrimRight(config.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	maxTokens := config.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	client := config.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return &Provider{apiKey: config.APIKey, baseURL: baseURL, maxTokens: maxTokens, httpClient: client}, nil
}

func (p *Provider) Name() string {
	return "anthropic"
}

// Complete performs a non-streaming completion. Symphony persists turn events around it.
func (p *Provider) Complete(ctx context.Context, request agent.CompletionRequest) (agent.Completion, error) {
	payload, err := toRequest(request, p.maxTokens)
	if err != nil {
		return agent.Completion{}, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return agent.Completion{}, fmt.Errorf("marshal Anthropic request: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return agent.Completion{}, fmt.Errorf("create Anthropic request: %w", err)
	}
	httpRequest.Header.Set("x-api-key", p.apiKey)
	httpRequest.Header.Set("anthropic-version", apiVersion)
	httpRequest.Header.Set("Content-Type", "application/json")

	response, err := p.httpClient.Do(httpRequest)
	if err != nil {
		return agent.Completion{}, fmt.Errorf("send Anthropic request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return agent.Completion{}, &Error{StatusCode: response.StatusCode, Detail: providers.ErrorDetail(response.Body, p.apiKey)}
	}

	var responsePayload messagesResponse
	if err := json.NewDecoder(response.Body).Decode(&responsePayload); err != nil {
		return agent.Completion{}, fmt.Errorf("decode Anthropic response: %w", err)
	}
	return toCompletion(responsePayload)
}

type messagesRequest struct {
	Model     string           `json:"model"`
	MaxTokens int              `json:"max_tokens"`
	System    string           `json:"system,omitempty"`
	Messages  []message        `json:"messages"`
	Tools     []toolDefinition `json:"tools,omitempty"`
}

type message struct {
	Role    agent.Role `json:"role"`
	Content any        `json:"content"`
}

type textBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolUseBlock struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type toolResultBlock struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}

type toolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

func toRequest(request agent.CompletionRequest, maxTokens int) (messagesRequest, error) {
	system := make([]string, 0)
	messages := make([]message, 0, len(request.Messages))
	for _, item := range request.Messages {
		switch item.Role {
		case agent.RoleSystem:
			system = append(system, item.Content)
		case agent.RoleUser, agent.RoleAssistant:
			messages = append(messages, message{Role: item.Role, Content: messageContent(item)})
		default:
			return messagesRequest{}, fmt.Errorf("unsupported Anthropic message role %q", item.Role)
		}
	}
	if len(messages) == 0 {
		return messagesRequest{}, errors.New("Anthropic request requires a user or assistant message")
	}
	tools := make([]toolDefinition, 0, len(request.Tools))
	for _, item := range request.Tools {
		tools = append(tools, toolDefinition{Name: item.Name, Description: item.Description, InputSchema: item.InputSchema})
	}
	return messagesRequest{
		Model:     request.Model,
		MaxTokens: maxTokens,
		System:    strings.Join(system, "\n\n"),
		Messages:  messages,
		Tools:     tools,
	}, nil
}

func messageContent(message agent.Message) any {
	if len(message.ToolCalls) == 0 && len(message.ToolResults) == 0 {
		return message.Content
	}
	blocks := make([]any, 0, 1+len(message.ToolCalls)+len(message.ToolResults))
	if message.Content != "" {
		blocks = append(blocks, textBlock{Type: "text", Text: message.Content})
	}
	for _, call := range message.ToolCalls {
		blocks = append(blocks, toolUseBlock{Type: "tool_use", ID: call.ID, Name: call.Name, Input: call.Arguments})
	}
	for _, result := range message.ToolResults {
		blocks = append(blocks, toolResultBlock{Type: "tool_result", ToolUseID: result.CallID, Content: result.Content, IsError: result.IsError})
	}
	return blocks
}

type messagesResponse struct {
	Content    []contentBlock `json:"content"`
	StopReason string         `json:"stop_reason"`
	Usage      usage          `json:"usage"`
}

type contentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

func toCompletion(response messagesResponse) (agent.Completion, error) {
	var content strings.Builder
	toolCalls := make([]events.ModelToolCall, 0)
	for _, block := range response.Content {
		switch block.Type {
		case "text":
			content.WriteString(block.Text)
		case "tool_use":
			input := block.Input
			if len(input) == 0 {
				input = json.RawMessage(`{}`)
			}
			var object map[string]any
			if err := json.Unmarshal(input, &object); err != nil || object == nil {
				return agent.Completion{}, errors.New("Anthropic tool input is not a JSON object")
			}
			toolCalls = append(toolCalls, events.ModelToolCall{ID: block.ID, Name: block.Name, Arguments: input})
		}
	}
	return agent.Completion{
		Content:      content.String(),
		ToolCalls:    toolCalls,
		StopReason:   response.StopReason,
		InputTokens:  response.Usage.InputTokens,
		OutputTokens: response.Usage.OutputTokens,
	}, nil
}
