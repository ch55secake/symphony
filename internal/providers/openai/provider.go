// Package openai implements the OpenAI Responses API for agent turns.
package openai

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
)

const defaultBaseURL = "https://api.openai.com/v1"

// Config supplies connection details. APIKey is never persisted by this package.
type Config struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
}

// Provider completes agent turns through the OpenAI Responses API.
type Provider struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// Error is a safe representation of a non-successful OpenAI response.
type Error struct {
	StatusCode int
}

func (e *Error) Error() string {
	return fmt.Sprintf("OpenAI response returned HTTP %d", e.StatusCode)
}

func New(config Config) (*Provider, error) {
	if strings.TrimSpace(config.APIKey) == "" {
		return nil, errors.New("OpenAI API key is required")
	}
	baseURL := strings.TrimRight(config.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	client := config.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return &Provider{apiKey: config.APIKey, baseURL: baseURL, httpClient: client}, nil
}

func (p *Provider) Name() string {
	return "openai"
}

// Complete performs a non-streaming completion. Symphony persists turn events around it.
func (p *Provider) Complete(ctx context.Context, request agent.CompletionRequest) (agent.Completion, error) {
	body, err := json.Marshal(toRequest(request))
	if err != nil {
		return agent.Completion{}, fmt.Errorf("marshal OpenAI request: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return agent.Completion{}, fmt.Errorf("create OpenAI request: %w", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")

	response, err := p.httpClient.Do(httpRequest)
	if err != nil {
		return agent.Completion{}, fmt.Errorf("send OpenAI request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return agent.Completion{}, &Error{StatusCode: response.StatusCode}
	}

	var payload responsesResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return agent.Completion{}, fmt.Errorf("decode OpenAI response: %w", err)
	}
	return toCompletion(payload)
}

type responsesRequest struct {
	Model string         `json:"model"`
	Input []any          `json:"input"`
	Tools []functionTool `json:"tools,omitempty"`
	Store bool           `json:"store"`
}

type inputMessage struct {
	Type    string     `json:"type"`
	Role    agent.Role `json:"role"`
	Content string     `json:"content"`
}

type functionTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type functionCallInput struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type functionCallOutputInput struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Output string `json:"output"`
}

func toRequest(request agent.CompletionRequest) responsesRequest {
	input := make([]any, 0, len(request.Messages))
	for _, message := range request.Messages {
		if message.Content != "" || (len(message.ToolCalls) == 0 && len(message.ToolResults) == 0) {
			input = append(input, inputMessage{Type: "message", Role: message.Role, Content: message.Content})
		}
		for _, call := range message.ToolCalls {
			input = append(input, functionCallInput{Type: "function_call", CallID: call.ID, Name: call.Name, Arguments: string(call.Arguments)})
		}
		for _, result := range message.ToolResults {
			input = append(input, functionCallOutputInput{Type: "function_call_output", CallID: result.CallID, Output: result.Content})
		}
	}
	tools := make([]functionTool, 0, len(request.Tools))
	for _, tool := range request.Tools {
		tools = append(tools, functionTool{
			Type:        "function",
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  tool.InputSchema,
		})
	}
	return responsesRequest{Model: request.Model, Input: input, Tools: tools, Store: false}
}

type responsesResponse struct {
	Status string           `json:"status"`
	Output []responseOutput `json:"output"`
	Usage  responseUsage    `json:"usage"`
}

type responseOutput struct {
	Type      string            `json:"type"`
	ID        string            `json:"id"`
	CallID    string            `json:"call_id"`
	Name      string            `json:"name"`
	Arguments string            `json:"arguments"`
	Content   []responseContent `json:"content"`
}

type responseContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responseUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

func toCompletion(response responsesResponse) (agent.Completion, error) {
	var content strings.Builder
	toolCalls := make([]events.ModelToolCall, 0)
	for _, output := range response.Output {
		switch output.Type {
		case "message":
			for _, part := range output.Content {
				if part.Type == "output_text" {
					content.WriteString(part.Text)
				}
			}
		case "function_call":
			arguments := json.RawMessage(output.Arguments)
			if len(arguments) == 0 {
				arguments = json.RawMessage(`{}`)
			}
			if !json.Valid(arguments) {
				return agent.Completion{}, errors.New("OpenAI function call arguments are not valid JSON")
			}
			callID := output.CallID
			if callID == "" {
				callID = output.ID
			}
			toolCalls = append(toolCalls, events.ModelToolCall{ID: callID, Name: output.Name, Arguments: arguments})
		}
	}
	return agent.Completion{
		Content:      content.String(),
		ToolCalls:    toolCalls,
		StopReason:   response.Status,
		InputTokens:  response.Usage.InputTokens,
		OutputTokens: response.Usage.OutputTokens,
	}, nil
}
