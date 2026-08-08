// Package opencode implements OpenCode Zen-compatible APIs for agent turns.
package opencode

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
	"github.com/ch55secake/symphony/internal/providers/openai"
)

const (
	defaultBaseURL     = "https://opencode.ai/zen/v1"
	TransportResponses = "responses"
	TransportChat      = "chat-completions"
)

// Config supplies connection details. APIKey is never persisted by this package.
type Config struct {
	APIKey     string
	BaseURL    string
	Transport  string
	HTTPClient *http.Client
}

// Provider completes agent turns through an OpenCode endpoint.
type Provider struct {
	transport  string
	responses  *openai.Provider
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// Error is a safe representation of a non-successful OpenCode response.
type Error struct {
	StatusCode int
	Detail     string
}

func (e *Error) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("OpenCode response returned HTTP %d: %s", e.StatusCode, e.Detail)
	}
	return fmt.Sprintf("OpenCode response returned HTTP %d", e.StatusCode)
}

func New(config Config) (*Provider, error) {
	if strings.TrimSpace(config.APIKey) == "" {
		return nil, errors.New("OpenCode API key is required")
	}
	baseURL := strings.TrimRight(config.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	transport := config.Transport
	if transport == "" {
		transport = TransportResponses
	}
	client := config.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	provider := &Provider{transport: transport, apiKey: config.APIKey, baseURL: baseURL, httpClient: client}
	switch transport {
	case TransportResponses:
		responses, err := openai.New(openai.Config{APIKey: config.APIKey, BaseURL: baseURL, HTTPClient: client})
		if err != nil {
			return nil, err
		}
		provider.responses = responses
	case TransportChat:
	default:
		return nil, fmt.Errorf("OpenCode transport must be %s or %s", TransportResponses, TransportChat)
	}
	return provider, nil
}

func (p *Provider) Name() string {
	return "opencode"
}

// Complete performs a non-streaming completion. Symphony persists turn events around it.
func (p *Provider) Complete(ctx context.Context, request agent.CompletionRequest) (agent.Completion, error) {
	if p.transport == TransportResponses {
		completion, err := p.responses.Complete(ctx, request)
		if err == nil {
			return completion, nil
		}
		var responseError *openai.Error
		if errors.As(err, &responseError) {
			return agent.Completion{}, &Error{StatusCode: responseError.StatusCode, Detail: responseError.Detail}
		}
		return agent.Completion{}, err
	}
	body, err := json.Marshal(toChatRequest(request))
	if err != nil {
		return agent.Completion{}, fmt.Errorf("marshal OpenCode chat request: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return agent.Completion{}, fmt.Errorf("create OpenCode chat request: %w", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")

	response, err := p.httpClient.Do(httpRequest)
	if err != nil {
		return agent.Completion{}, fmt.Errorf("send OpenCode chat request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return agent.Completion{}, &Error{StatusCode: response.StatusCode, Detail: providers.ErrorDetail(response.Body, p.apiKey)}
	}
	var payload chatResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return agent.Completion{}, fmt.Errorf("decode OpenCode chat response: %w", err)
	}
	return toChatCompletion(payload)
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Tools    []chatTool    `json:"tools,omitempty"`
}

type chatMessage struct {
	Role       agent.Role            `json:"role"`
	Content    string                `json:"content,omitempty"`
	ToolCalls  []chatRequestToolCall `json:"tool_calls,omitempty"`
	ToolCallID string                `json:"tool_call_id,omitempty"`
}

type chatTool struct {
	Type     string       `json:"type"`
	Function chatFunction `json:"function"`
}

type chatFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type chatRequestToolCall struct {
	ID       string              `json:"id"`
	Type     string              `json:"type"`
	Function chatRequestFunction `json:"function"`
}

type chatRequestFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func toChatRequest(request agent.CompletionRequest) chatRequest {
	messages := make([]chatMessage, 0, len(request.Messages))
	for _, message := range request.Messages {
		if message.Content != "" || (len(message.ToolCalls) == 0 && len(message.ToolResults) == 0) {
			messages = append(messages, chatMessage{Role: message.Role, Content: message.Content})
		}
		if len(message.ToolCalls) > 0 {
			calls := make([]chatRequestToolCall, 0, len(message.ToolCalls))
			for _, call := range message.ToolCalls {
				calls = append(calls, chatRequestToolCall{ID: call.ID, Type: "function", Function: chatRequestFunction{Name: call.Name, Arguments: string(call.Arguments)}})
			}
			messages = append(messages, chatMessage{Role: agent.RoleAssistant, ToolCalls: calls})
		}
		for _, result := range message.ToolResults {
			messages = append(messages, chatMessage{Role: agent.Role("tool"), Content: result.Content, ToolCallID: result.CallID})
		}
	}
	tools := make([]chatTool, 0, len(request.Tools))
	for _, tool := range request.Tools {
		tools = append(tools, chatTool{Type: "function", Function: chatFunction{Name: tool.Name, Description: tool.Description, Parameters: tool.InputSchema}})
	}
	return chatRequest{Model: request.Model, Messages: messages, Tools: tools}
}

type chatResponse struct {
	Choices []chatChoice `json:"choices"`
	Usage   chatUsage    `json:"usage"`
}

type chatChoice struct {
	Message      chatResponseMessage `json:"message"`
	FinishReason string              `json:"finish_reason"`
}

type chatResponseMessage struct {
	Content   string                 `json:"content"`
	ToolCalls []chatResponseToolCall `json:"tool_calls"`
}

type chatResponseToolCall struct {
	ID       string               `json:"id"`
	Function chatResponseFunction `json:"function"`
}

type chatResponseFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

func toChatCompletion(response chatResponse) (agent.Completion, error) {
	if len(response.Choices) == 0 {
		return agent.Completion{}, errors.New("OpenCode chat response has no choices")
	}
	choice := response.Choices[0]
	toolCalls := make([]events.ModelToolCall, 0, len(choice.Message.ToolCalls))
	for _, call := range choice.Message.ToolCalls {
		arguments := json.RawMessage(call.Function.Arguments)
		if len(arguments) == 0 {
			arguments = json.RawMessage(`{}`)
		}
		var object map[string]any
		if err := json.Unmarshal(arguments, &object); err != nil || object == nil {
			return agent.Completion{}, errors.New("OpenCode chat function arguments are not a JSON object")
		}
		toolCalls = append(toolCalls, events.ModelToolCall{ID: call.ID, Name: call.Function.Name, Arguments: arguments})
	}
	return agent.Completion{
		Content:      choice.Message.Content,
		ToolCalls:    toolCalls,
		StopReason:   choice.FinishReason,
		InputTokens:  response.Usage.PromptTokens,
		OutputTokens: response.Usage.CompletionTokens,
	}, nil
}
