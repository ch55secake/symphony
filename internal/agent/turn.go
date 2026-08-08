// Package agent orchestrates audited model turns for a Symphony session.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ch55secake/symphony/internal/events"
	"github.com/ch55secake/symphony/internal/session"
)

// Role identifies the speaker for a model message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is provider-neutral conversation content.
type Message struct {
	Role        Role                   `json:"role"`
	Content     string                 `json:"content,omitempty"`
	ToolCalls   []events.ModelToolCall `json:"tool_calls,omitempty"`
	ToolResults []ToolResult           `json:"tool_results,omitempty"`
}

// ToolResult is the bounded output returned to a model after a tool execution.
type ToolResult struct {
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error"`
	Bytes     int    `json:"bytes"`
	Hash      string `json:"hash"`
	Truncated bool   `json:"truncated"`
}

// ToolDefinition describes a callable tool exposed to a model.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// CompletionRequest is a provider-neutral model completion request.
type CompletionRequest struct {
	Model    string           `json:"model"`
	Messages []Message        `json:"messages"`
	Tools    []ToolDefinition `json:"tools,omitempty"`
}

// Completion is a provider-neutral model completion response.
type Completion struct {
	Content      string                 `json:"content"`
	ToolCalls    []events.ModelToolCall `json:"tool_calls,omitempty"`
	StopReason   string                 `json:"stop_reason"`
	InputTokens  int                    `json:"input_tokens,omitempty"`
	OutputTokens int                    `json:"output_tokens,omitempty"`
}

// Provider completes a turn. Credentials remain implementation configuration.
type Provider interface {
	Name() string
	Complete(context.Context, CompletionRequest) (Completion, error)
}

// Service records turn state before and after provider side effects.
type Service struct {
	sessions *session.Service
}

func New(sessions *session.Service) (*Service, error) {
	if sessions == nil {
		return nil, fmt.Errorf("session service is required")
	}
	return &Service{sessions: sessions}, nil
}

// Run records user input, model intent, and the provider outcome for one turn.
func (s *Service) Run(ctx context.Context, handle *session.Handle, actor string, provider Provider, request CompletionRequest) (Completion, error) {
	return s.run(ctx, handle, actor, provider, request, true)
}

// Continue records a follow-up model turn without duplicating prior user messages.
func (s *Service) Continue(ctx context.Context, handle *session.Handle, actor string, provider Provider, request CompletionRequest) (Completion, error) {
	return s.run(ctx, handle, actor, provider, request, false)
}

func (s *Service) run(ctx context.Context, handle *session.Handle, actor string, provider Provider, request CompletionRequest, recordUserMessages bool) (Completion, error) {
	if provider == nil {
		return Completion{}, fmt.Errorf("model provider is required")
	}
	if request.Model == "" {
		return Completion{}, fmt.Errorf("model is required")
	}
	if len(request.Messages) == 0 {
		return Completion{}, fmt.Errorf("at least one message is required")
	}
	if recordUserMessages {
		for _, message := range request.Messages {
			if message.Role != RoleUser || message.Content == "" {
				continue
			}
			if err := s.sessions.Record(ctx, handle, events.UserMessage, actor, events.UserMessagePayload{Content: message.Content}); err != nil {
				return Completion{}, fmt.Errorf("record user message: %w", err)
			}
		}
	}

	requestHash, err := hashRequest(request)
	if err != nil {
		return Completion{}, err
	}
	if err := s.sessions.Record(ctx, handle, events.ModelRequested, provider.Name(), events.ModelRequestedPayload{
		Provider:     provider.Name(),
		Model:        request.Model,
		RequestHash:  requestHash,
		MessageCount: len(request.Messages),
		ToolNames:    toolNames(request.Tools),
	}); err != nil {
		return Completion{}, fmt.Errorf("record model intent: %w", err)
	}

	completion, err := provider.Complete(ctx, request)
	if err != nil {
		outcomeErr := s.sessions.Record(ctx, handle, events.ModelFailed, provider.Name(), events.ModelFailedPayload{
			Provider: provider.Name(),
			Model:    request.Model,
			Code:     failureCode(ctx, err),
		})
		if outcomeErr != nil {
			return Completion{}, errors.Join(fmt.Errorf("complete model turn: %w", err), fmt.Errorf("record model failure: %w", outcomeErr))
		}
		return Completion{}, fmt.Errorf("complete model turn: %w", err)
	}
	if err := s.sessions.Record(ctx, handle, events.ModelCompleted, provider.Name(), events.ModelCompletedPayload{
		Provider:     provider.Name(),
		Model:        request.Model,
		Content:      completion.Content,
		ToolCalls:    completion.ToolCalls,
		StopReason:   completion.StopReason,
		InputTokens:  completion.InputTokens,
		OutputTokens: completion.OutputTokens,
	}); err != nil {
		return Completion{}, fmt.Errorf("record model completion: %w", err)
	}
	return completion, nil
}

// RecordToolResult persists a tool result before it is included in a follow-up request.
func (s *Service) RecordToolResult(ctx context.Context, handle *session.Handle, actor string, result ToolResult) error {
	return s.sessions.Record(ctx, handle, events.ToolResult, actor, events.ToolResultPayload{
		CallID:    result.CallID,
		Name:      result.Name,
		IsError:   result.IsError,
		Bytes:     result.Bytes,
		Hash:      result.Hash,
		Truncated: result.Truncated,
	})
}

// RecordApproval persists an operator decision before an approved action executes.
func (s *Service) RecordApproval(ctx context.Context, handle *session.Handle, actor string, eventType events.Type, payload any) error {
	return s.sessions.Record(ctx, handle, eventType, actor, payload)
}

func hashRequest(request CompletionRequest) (string, error) {
	encoded, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("marshal completion request: %w", err)
	}
	return events.Hash(encoded), nil
}

func toolNames(tools []ToolDefinition) []string {
	if len(tools) == 0 {
		return nil
	}
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

func failureCode(ctx context.Context, err error) string {
	if ctx.Err() != nil {
		return "canceled"
	}
	return "provider_error"
}
