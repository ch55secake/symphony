// Package opencode implements OpenCode Zen-compatible APIs for agent turns.
package opencode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	defer func() { _ = response.Body.Close() }()
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
	Model     string         `json:"model"`
	Messages  []chatMessage  `json:"messages"`
	Tools     []chatTool     `json:"tools,omitempty"`
	Stream    bool           `json:"stream,omitempty"`
	Reasoning *chatReasoning `json:"reasoning,omitempty"`
}

type chatReasoning struct {
	Enabled bool `json:"enabled,omitempty"`
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
	messages := make([]chatMessage, 0, len(request.Messages)+1)
	if request.Instructions != "" {
		messages = append(messages, chatMessage{Role: agent.RoleSystem, Content: request.Instructions})
	}
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
	result := chatRequest{Model: request.Model, Messages: messages, Tools: tools}
	if request.ReasoningSummaries {
		result.Reasoning = &chatReasoning{Enabled: true}
	}
	return result
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

// CompleteStream streams either the Responses or Chat Completions transport.
func (p *Provider) CompleteStream(ctx context.Context, request agent.CompletionRequest, observer agent.StreamObserver) (agent.Completion, error) {
	if p.transport == TransportResponses {
		completion, err := p.responses.CompleteStream(ctx, request, observer)
		if err == nil {
			return completion, nil
		}
		var responseError *openai.Error
		if errors.As(err, &responseError) {
			return agent.Completion{}, &Error{StatusCode: responseError.StatusCode, Detail: responseError.Detail}
		}
		return agent.Completion{}, err
	}
	payload := toChatRequest(request)
	payload.Stream = true
	body, err := json.Marshal(payload)
	if err != nil {
		return agent.Completion{}, fmt.Errorf("marshal OpenCode chat streaming request: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return agent.Completion{}, fmt.Errorf("create OpenCode chat streaming request: %w", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := p.httpClient.Do(httpRequest)
	if err != nil {
		return agent.Completion{}, fmt.Errorf("send OpenCode chat streaming request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return agent.Completion{}, &Error{StatusCode: response.StatusCode, Detail: providers.ErrorDetail(response.Body, p.apiKey)}
	}

	type toolCall struct {
		ID, Name  string
		Arguments strings.Builder
	}
	tools := make(map[int]*toolCall)
	var content strings.Builder
	completion := agent.Completion{}
	reader := bufio.NewReader(response.Body)
	for {
		event, readErr := providers.ReadSSE(reader)
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return agent.Completion{}, fmt.Errorf("read OpenCode chat stream: %w", readErr)
		}
		if string(event.Data) == "[DONE]" {
			break
		}
		var envelope struct {
			Choices []struct {
				Delta struct {
					Content          string            `json:"content"`
					Reasoning        string            `json:"reasoning"`
					ReasoningContent string            `json:"reasoning_content"`
					ReasoningDetails []json.RawMessage `json:"reasoning_details"`
					ToolCalls        []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if err := providers.DecodeSSE(event, &envelope); err != nil {
			return agent.Completion{}, err
		}
		for _, choice := range envelope.Choices {
			if reasoning := chatReasoningDelta(choice.Delta.Reasoning, choice.Delta.ReasoningContent, choice.Delta.ReasoningDetails); reasoning != "" {
				emitStream(observer, agent.StreamEvent{Kind: agent.StreamReasoning, Text: reasoning})
			}
			if choice.Delta.Content != "" {
				content.WriteString(choice.Delta.Content)
				emitStream(observer, agent.StreamEvent{Kind: agent.StreamText, Text: choice.Delta.Content})
			}
			if choice.FinishReason != "" {
				completion.StopReason = choice.FinishReason
			}
			for _, delta := range choice.Delta.ToolCalls {
				current := tools[delta.Index]
				if current == nil {
					current = &toolCall{}
					tools[delta.Index] = current
				}
				if delta.ID != "" {
					current.ID = delta.ID
				}
				if delta.Function.Name != "" {
					current.Name = delta.Function.Name
				}
				current.Arguments.WriteString(delta.Function.Arguments)
			}
		}
		completion.InputTokens = envelope.Usage.PromptTokens
		completion.OutputTokens = envelope.Usage.CompletionTokens
	}
	completion.Content = content.String()
	for _, current := range tools {
		arguments := []byte(current.Arguments.String())
		if len(arguments) == 0 {
			arguments = []byte(`{}`)
		}
		var object map[string]any
		if err := json.Unmarshal(arguments, &object); err != nil || object == nil {
			return agent.Completion{}, errors.New("OpenCode streamed function arguments are not a JSON object")
		}
		completion.ToolCalls = append(completion.ToolCalls, events.ModelToolCall{ID: current.ID, Name: current.Name, Arguments: arguments})
	}
	return completion, nil
}

func chatReasoningDelta(reasoning, reasoningContent string, details []json.RawMessage) string {
	if reasoning != "" {
		return reasoning
	}
	if reasoningContent != "" {
		return reasoningContent
	}
	var text strings.Builder
	for _, raw := range details {
		var detail struct {
			Type    string `json:"type"`
			Summary string `json:"summary"`
			Text    string `json:"text"`
		}
		if json.Unmarshal(raw, &detail) != nil {
			continue
		}
		switch detail.Type {
		case "reasoning.summary", "reasoning.text":
			if detail.Summary != "" {
				text.WriteString(detail.Summary)
			} else {
				text.WriteString(detail.Text)
			}
		}
	}
	return text.String()
}

func emitStream(observer agent.StreamObserver, event agent.StreamEvent) {
	if observer != nil && event.Text != "" {
		observer(event)
	}
}
