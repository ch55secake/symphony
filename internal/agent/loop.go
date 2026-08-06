package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/ch55secake/symphony/internal/events"
	"github.com/ch55secake/symphony/internal/session"
	"github.com/ch55secake/symphony/internal/workspace"
	"github.com/google/uuid"
)

const defaultMaxToolRounds = 8

var (
	ErrUnknownTool     = errors.New("model requested an unknown tool")
	ErrMaxToolRounds   = errors.New("maximum tool rounds exceeded")
	ErrApprovalPending = errors.New("action approval is pending")
	ErrApprovalUsed    = errors.New("approval has already been resolved")
	ErrApprovalSession = errors.New("approval belongs to another session")
	ErrMixedToolCalls  = errors.New("approval-required tool call must be the only tool call in a turn")
)

// Tool dispatches a provider-requested call to a native Symphony capability.
type Tool interface {
	Definition() ToolDefinition
	Execute(context.Context, *session.Handle, string, json.RawMessage) (ToolResult, error)
}

// ApprovalTool stages a side effect and returns a pending operator approval.
type ApprovalTool interface {
	Tool
	RequestApproval(context.Context, *session.Handle, string, json.RawMessage) (*PendingApproval, error)
}

// LoopResult contains either a final completion or a pending approval.
type LoopResult struct {
	Completion *Completion
	Pending    *PendingApproval
}

// PendingApproval exposes safe metadata while retaining execution data in memory only.
type PendingApproval struct {
	mu           sync.Mutex
	OperationID  string
	Action       string
	Summary      string
	Hash         string
	ToolCallID   string
	sessionID    uuid.UUID
	used         bool
	approve      func(context.Context, *session.Handle, string) (ToolResult, error)
	continuation CompletionRequest
	round        int
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
	result, err := l.RunWithApproval(ctx, handle, actor, provider, request)
	if err != nil {
		return Completion{}, err
	}
	if result.Pending != nil {
		return Completion{}, ErrApprovalPending
	}
	return *result.Completion, nil
}

// RunWithApproval runs until final completion or an action needs approval.
func (l *Loop) RunWithApproval(ctx context.Context, handle *session.Handle, actor string, provider Provider, request CompletionRequest) (LoopResult, error) {
	return l.run(ctx, handle, actor, provider, request, 0)
}

// Approve persists approval, executes the staged action, and resumes the loop.
func (l *Loop) Approve(ctx context.Context, handle *session.Handle, actor string, provider Provider, pending *PendingApproval) (LoopResult, error) {
	if err := pending.begin(handle); err != nil {
		return LoopResult{}, err
	}
	if err := l.turns.RecordApproval(ctx, handle, actor, events.ApprovalGranted, events.ApprovalGrantedPayload{
		OperationID: pending.OperationID,
		Action:      pending.Action,
	}); err != nil {
		pending.release()
		return LoopResult{}, fmt.Errorf("record approval grant: %w", err)
	}
	pending.consume()
	result, err := pending.approve(ctx, handle, actor)
	if err != nil {
		return LoopResult{}, fmt.Errorf("execute approved action: %w", err)
	}
	if err := l.turns.RecordToolResult(ctx, handle, pending.Action, result); err != nil {
		return LoopResult{}, fmt.Errorf("record approved tool result: %w", err)
	}
	return l.resume(ctx, handle, actor, provider, pending, result)
}

// Deny persists denial and resumes the loop with an error tool result.
func (l *Loop) Deny(ctx context.Context, handle *session.Handle, actor string, provider Provider, pending *PendingApproval, reasonCode string) (LoopResult, error) {
	if err := pending.begin(handle); err != nil {
		return LoopResult{}, err
	}
	if reasonCode == "" {
		reasonCode = "denied"
	}
	if err := l.turns.RecordApproval(ctx, handle, actor, events.ApprovalDenied, events.ApprovalDeniedPayload{
		OperationID: pending.OperationID,
		Action:      pending.Action,
		ReasonCode:  reasonCode,
	}); err != nil {
		pending.release()
		return LoopResult{}, fmt.Errorf("record approval denial: %w", err)
	}
	pending.consume()
	result := ToolResult{
		CallID:  pending.ToolCallID,
		Name:    pending.Action,
		Content: "action denied",
		IsError: true,
		Bytes:   len("action denied"),
		Hash:    events.Hash([]byte("action denied")),
	}
	if err := l.turns.RecordToolResult(ctx, handle, pending.Action, result); err != nil {
		return LoopResult{}, fmt.Errorf("record denied tool result: %w", err)
	}
	return l.resume(ctx, handle, actor, provider, pending, result)
}

func (l *Loop) resume(ctx context.Context, handle *session.Handle, actor string, provider Provider, pending *PendingApproval, result ToolResult) (LoopResult, error) {
	pending.continuation.Messages = append(pending.continuation.Messages, Message{Role: RoleUser, ToolResults: []ToolResult{result}})
	return l.run(ctx, handle, actor, provider, pending.continuation, pending.round)
}

func (l *Loop) run(ctx context.Context, handle *session.Handle, actor string, provider Provider, request CompletionRequest, startRound int) (LoopResult, error) {
	current := request
	for round := startRound; ; round++ {
		var completion Completion
		var err error
		if round == 0 {
			completion, err = l.turns.Run(ctx, handle, actor, provider, current)
		} else {
			completion, err = l.turns.Continue(ctx, handle, actor, provider, current)
		}
		if err != nil {
			return LoopResult{}, err
		}
		if len(completion.ToolCalls) == 0 {
			return LoopResult{Completion: &completion}, nil
		}
		if round >= l.maxToolRounds {
			return LoopResult{}, ErrMaxToolRounds
		}
		if pending, ok, err := l.requestApproval(ctx, handle, actor, current, completion, round); err != nil {
			return LoopResult{}, err
		} else if ok {
			return LoopResult{Pending: pending}, nil
		}

		results := make([]ToolResult, 0, len(completion.ToolCalls))
		for _, call := range completion.ToolCalls {
			tool, exists := l.tools[call.Name]
			if !exists {
				result := failedToolResult(call, "unknown tool")
				if err := l.turns.RecordToolResult(ctx, handle, call.Name, result); err != nil {
					return LoopResult{}, fmt.Errorf("record unknown tool result: %w", err)
				}
				return LoopResult{}, fmt.Errorf("%w: %s", ErrUnknownTool, call.Name)
			}
			result, toolErr := tool.Execute(ctx, handle, call.Name, call.Arguments)
			result.CallID = call.ID
			result.Name = call.Name
			if err := l.turns.RecordToolResult(ctx, handle, call.Name, result); err != nil {
				return LoopResult{}, fmt.Errorf("record tool result: %w", err)
			}
			if toolErr != nil {
				return LoopResult{}, fmt.Errorf("execute tool %q: %w", call.Name, toolErr)
			}
			results = append(results, result)
		}
		current.Messages = append(current.Messages,
			Message{Role: RoleAssistant, Content: completion.Content, ToolCalls: completion.ToolCalls},
			Message{Role: RoleUser, ToolResults: results},
		)
	}
}

func (l *Loop) requestApproval(ctx context.Context, handle *session.Handle, actor string, request CompletionRequest, completion Completion, round int) (*PendingApproval, bool, error) {
	for _, call := range completion.ToolCalls {
		tool, exists := l.tools[call.Name]
		if !exists {
			continue
		}
		approvalTool, needsApproval := tool.(ApprovalTool)
		if !needsApproval {
			continue
		}
		if len(completion.ToolCalls) != 1 {
			return nil, false, ErrMixedToolCalls
		}
		pending, err := approvalTool.RequestApproval(ctx, handle, actor, call.Arguments)
		if err != nil {
			return nil, false, fmt.Errorf("request approval for tool %q: %w", call.Name, err)
		}
		pending.ToolCallID = call.ID
		pending.sessionID = handle.SessionID
		pending.continuation = request
		pending.continuation.Messages = append(pending.continuation.Messages, Message{Role: RoleAssistant, Content: completion.Content, ToolCalls: completion.ToolCalls})
		pending.round = round + 1
		if err := l.turns.RecordApproval(ctx, handle, actor, events.ApprovalRequested, events.ApprovalRequestedPayload{
			OperationID: pending.OperationID,
			Action:      pending.Action,
			Summary:     pending.Summary,
			Hash:        pending.Hash,
		}); err != nil {
			return nil, false, fmt.Errorf("record approval request: %w", err)
		}
		return pending, true, nil
	}
	return nil, false, nil
}

func (p *PendingApproval) begin(handle *session.Handle) error {
	if p == nil || handle == nil {
		return errors.New("pending approval and session handle are required")
	}
	p.mu.Lock()
	if p.used {
		p.mu.Unlock()
		return ErrApprovalUsed
	}
	if p.sessionID != handle.SessionID {
		p.mu.Unlock()
		return ErrApprovalSession
	}
	return nil
}

func (p *PendingApproval) release() {
	p.mu.Unlock()
}

func (p *PendingApproval) consume() {
	p.used = true
	p.mu.Unlock()
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

// WriteFileTool stages an audited file write for explicit operator approval.
type WriteFileTool struct {
	workspace *workspace.Service
}

func NewWriteFileTool(workspaceService *workspace.Service) (*WriteFileTool, error) {
	if workspaceService == nil {
		return nil, fmt.Errorf("workspace service is required")
	}
	return &WriteFileTool{workspace: workspaceService}, nil
}

func (t *WriteFileTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "write_file",
		Description: "Write UTF-8 content to a workspace file after operator approval.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"],"additionalProperties":false}`),
	}
}

// Execute is unreachable during normal loop operation because writes always pause first.
func (t *WriteFileTool) Execute(_ context.Context, _ *session.Handle, _ string, _ json.RawMessage) (ToolResult, error) {
	return ToolResult{}, ErrApprovalPending
}

func (t *WriteFileTool) RequestApproval(ctx context.Context, handle *session.Handle, actor string, arguments json.RawMessage) (*PendingApproval, error) {
	var input struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(arguments, &input); err != nil || input.Path == "" {
		return nil, errors.New("invalid write_file arguments")
	}
	content := []byte(input.Content)
	request, err := t.workspace.RequestWrite(ctx, handle, actor, input.Path, content)
	if err != nil {
		return nil, err
	}
	return &PendingApproval{
		OperationID: request.OperationID.String(),
		Action:      "write_file",
		Summary:     fmt.Sprintf("write %s (%d bytes)", request.Path, request.Bytes),
		Hash:        request.ContentHash,
		approve: func(ctx context.Context, handle *session.Handle, actor string) (ToolResult, error) {
			if err := t.workspace.ApproveWrite(ctx, handle, actor, request); err != nil {
				return ToolResult{}, err
			}
			if err := t.workspace.ExecuteWrite(ctx, handle, actor, request, content); err != nil {
				return ToolResult{}, err
			}
			return ToolResult{
				Content: "write_file completed",
				Bytes:   request.Bytes,
				Hash:    request.ContentHash,
			}, nil
		},
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
