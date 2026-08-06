package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ch55secake/symphony/internal/events"
	"github.com/ch55secake/symphony/internal/session"
	"github.com/ch55secake/symphony/internal/workspace"
)

const defaultMaxToolRounds = 8

var (
	ErrUnknownTool   = errors.New("model requested an unknown tool")
	ErrMaxToolRounds = errors.New("maximum tool rounds exceeded")
)

// Tool dispatches a provider-requested call to a native Symphony capability.
type Tool interface {
	Definition() ToolDefinition
	Execute(context.Context, *session.Handle, string, json.RawMessage) (ToolResult, error)
}

// Loop runs provider turns and registered read-only tool calls until completion.
type Loop struct {
	turns         *Service
	tools         map[string]Tool
	maxToolRounds int
}

func NewLoop(turns *Service, tools []Tool, maxToolRounds int) (*Loop, error) {
	if turns == nil {
		return nil, fmt.Errorf("agent turn service is required")
	}
	if maxToolRounds <= 0 {
		maxToolRounds = defaultMaxToolRounds
	}
	registered := make(map[string]Tool, len(tools))
	for _, tool := range tools {
		if tool == nil {
			return nil, fmt.Errorf("tool is required")
		}
		definition := tool.Definition()
		if definition.Name == "" {
			return nil, fmt.Errorf("tool name is required")
		}
		if _, exists := registered[definition.Name]; exists {
			return nil, fmt.Errorf("duplicate tool %q", definition.Name)
		}
		registered[definition.Name] = tool
	}
	return &Loop{turns: turns, tools: registered, maxToolRounds: maxToolRounds}, nil
}

// Run continues model turns while registered tool calls are returned.
func (l *Loop) Run(ctx context.Context, handle *session.Handle, actor string, provider Provider, request CompletionRequest) (Completion, error) {
	current := request
	for round := 0; ; round++ {
		var completion Completion
		var err error
		if round == 0 {
			completion, err = l.turns.Run(ctx, handle, actor, provider, current)
		} else {
			completion, err = l.turns.Continue(ctx, handle, actor, provider, current)
		}
		if err != nil {
			return Completion{}, err
		}
		if len(completion.ToolCalls) == 0 {
			return completion, nil
		}
		if round >= l.maxToolRounds {
			return Completion{}, ErrMaxToolRounds
		}

		results := make([]ToolResult, 0, len(completion.ToolCalls))
		for _, call := range completion.ToolCalls {
			tool, exists := l.tools[call.Name]
			if !exists {
				result := failedToolResult(call, "unknown tool")
				if err := l.turns.RecordToolResult(ctx, handle, call.Name, result); err != nil {
					return Completion{}, fmt.Errorf("record unknown tool result: %w", err)
				}
				return Completion{}, fmt.Errorf("%w: %s", ErrUnknownTool, call.Name)
			}
			result, toolErr := tool.Execute(ctx, handle, call.Name, call.Arguments)
			result.CallID = call.ID
			result.Name = call.Name
			if err := l.turns.RecordToolResult(ctx, handle, call.Name, result); err != nil {
				return Completion{}, fmt.Errorf("record tool result: %w", err)
			}
			if toolErr != nil {
				return Completion{}, fmt.Errorf("execute tool %q: %w", call.Name, toolErr)
			}
			results = append(results, result)
		}
		current.Messages = append(current.Messages,
			Message{Role: RoleAssistant, Content: completion.Content, ToolCalls: completion.ToolCalls},
			Message{Role: RoleUser, ToolResults: results},
		)
	}
}

// ReadFileTool exposes the audited workspace read service to a provider.
type ReadFileTool struct {
	workspace *workspace.Service
	maxBytes  int
}

func NewReadFileTool(workspaceService *workspace.Service, maxBytes int) (*ReadFileTool, error) {
	if workspaceService == nil {
		return nil, fmt.Errorf("workspace service is required")
	}
	if maxBytes <= 0 {
		maxBytes = 64 << 10
	}
	return &ReadFileTool{workspace: workspaceService, maxBytes: maxBytes}, nil
}

func (t *ReadFileTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "read_file",
		Description: "Read a UTF-8 workspace file by relative path.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`),
	}
}

func (t *ReadFileTool) Execute(ctx context.Context, handle *session.Handle, actor string, arguments json.RawMessage) (ToolResult, error) {
	var input struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(arguments, &input); err != nil || input.Path == "" {
		return failedToolResult(events.ModelToolCall{Name: "read_file"}, "invalid read_file arguments"), errors.New("invalid read_file arguments")
	}
	data, err := t.workspace.Read(ctx, handle, actor, input.Path)
	if err != nil {
		return failedToolResult(events.ModelToolCall{Name: "read_file"}, "read_file failed"), err
	}
	content := data
	truncated := false
	if len(content) > t.maxBytes {
		content = content[:t.maxBytes]
		truncated = true
	}
	return ToolResult{
		Content:   string(content),
		Bytes:     len(data),
		Hash:      events.Hash(data),
		Truncated: truncated,
	}, nil
}

func failedToolResult(call events.ModelToolCall, content string) ToolResult {
	return ToolResult{
		CallID:  call.ID,
		Name:    call.Name,
		Content: content,
		IsError: true,
		Bytes:   len(content),
		Hash:    events.Hash([]byte(content)),
	}
}
