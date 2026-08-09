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
	ErrToolUnavailable = errors.New("tool is unavailable in the active mode")
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
	// Messages retains the in-memory conversation, including transient tool
	// results, so an interactive caller can continue the same session.
	Messages []Message
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
	activity     ToolActivity
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
	return l.RunWithApprovalObserved(ctx, handle, actor, provider, request, nil)
}

// RunWithApprovalObserved runs until completion or approval while reporting safe tool activity.
func (l *Loop) RunWithApprovalObserved(ctx context.Context, handle *session.Handle, actor string, provider Provider, request CompletionRequest, observer ActivityObserver) (LoopResult, error) {
	return l.run(ctx, handle, actor, provider, request, 0, observer)
}

// Approve persists approval, executes the staged action, and resumes the loop.
func (l *Loop) Approve(ctx context.Context, handle *session.Handle, actor string, provider Provider, pending *PendingApproval) (LoopResult, error) {
	return l.ApproveObserved(ctx, handle, actor, provider, pending, nil)
}

// ApproveObserved approves an action while reporting safe tool activity.
func (l *Loop) ApproveObserved(ctx context.Context, handle *session.Handle, actor string, provider Provider, pending *PendingApproval, observer ActivityObserver) (LoopResult, error) {
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
	pending.activity.Phase = ActivityRunning
	emitActivity(observer, pending.activity)
	result, err := pending.approve(ctx, handle, actor)
	result.CallID = pending.ToolCallID
	result.Name = pending.Action
	if err != nil {
		recoverable := errors.Is(err, workspace.ErrCommandExecutionFailed) && ctx.Err() == nil
		if result.Content == "" {
			result.Content = pending.Action + " failed"
			result.Bytes = len(result.Content)
			result.Hash = events.Hash([]byte(result.Content))
		}
		result.IsError = true
		emitActivity(observer, completeActivity(pending.activity, result, ActivityFailed))
		recordErr := l.recordToolResult(ctx, handle, pending.Action, result)
		messages := append([]Message(nil), pending.continuation.Messages...)
		messages = append(messages, Message{Role: RoleUser, ToolResults: []ToolResult{result}})
		if recordErr != nil {
			return LoopResult{Messages: messages}, errors.Join(fmt.Errorf("execute approved action: %w", err), fmt.Errorf("record failed tool result: %w", recordErr))
		}
		if recoverable {
			return l.resume(ctx, handle, actor, provider, pending, result, observer)
		}
		return LoopResult{Messages: messages}, fmt.Errorf("execute approved action: %w", err)
	}
	emitActivity(observer, completeActivity(pending.activity, result, ActivityCompleted))
	if err := l.recordToolResult(ctx, handle, pending.Action, result); err != nil {
		return LoopResult{}, fmt.Errorf("record approved tool result: %w", err)
	}
	return l.resume(ctx, handle, actor, provider, pending, result, observer)
}

// Deny persists denial and resumes the loop with an error tool result.
func (l *Loop) Deny(ctx context.Context, handle *session.Handle, actor string, provider Provider, pending *PendingApproval, reasonCode string) (LoopResult, error) {
	return l.DenyObserved(ctx, handle, actor, provider, pending, reasonCode, nil)
}

// DenyObserved denies an action while reporting safe tool activity.
func (l *Loop) DenyObserved(ctx context.Context, handle *session.Handle, actor string, provider Provider, pending *PendingApproval, reasonCode string, observer ActivityObserver) (LoopResult, error) {
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
	emitActivity(observer, completeActivity(pending.activity, result, ActivityDenied))
	if err := l.recordToolResult(ctx, handle, pending.Action, result); err != nil {
		return LoopResult{}, fmt.Errorf("record denied tool result: %w", err)
	}
	return l.resume(ctx, handle, actor, provider, pending, result, observer)
}

func (l *Loop) resume(ctx context.Context, handle *session.Handle, actor string, provider Provider, pending *PendingApproval, result ToolResult, observer ActivityObserver) (LoopResult, error) {
	pending.continuation.Messages = append(pending.continuation.Messages, Message{Role: RoleUser, ToolResults: []ToolResult{result}})
	resumed, err := l.run(ctx, handle, actor, provider, pending.continuation, pending.round, observer)
	if err != nil && len(resumed.Messages) == 0 {
		resumed.Messages = append([]Message(nil), pending.continuation.Messages...)
	}
	return resumed, err
}

func (l *Loop) run(ctx context.Context, handle *session.Handle, actor string, provider Provider, request CompletionRequest, startRound int, observer ActivityObserver) (LoopResult, error) {
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
			return LoopResult{Messages: append([]Message(nil), current.Messages...)}, err
		}
		if len(completion.ToolCalls) == 0 {
			messages := append([]Message(nil), current.Messages...)
			messages = append(messages, Message{
				Role:    RoleAssistant,
				Content: completion.Content,
			})
			return LoopResult{Completion: &completion, Messages: messages}, nil
		}
		if round >= l.maxToolRounds {
			return LoopResult{}, ErrMaxToolRounds
		}
		for _, call := range completion.ToolCalls {
			if !toolDefinitionAvailable(current.Tools, call.Name) {
				result := failedToolResult(call, "tool is unavailable in the active mode")
				if err := l.recordToolResult(ctx, handle, call.Name, result); err != nil {
					return LoopResult{Messages: toolConversation(current.Messages, completion, []ToolResult{result})}, fmt.Errorf("record unavailable tool result: %w", err)
				}
				return LoopResult{Messages: toolConversation(current.Messages, completion, []ToolResult{result})}, fmt.Errorf("%w: %s", ErrToolUnavailable, call.Name)
			}
		}
		for _, call := range completion.ToolCalls {
			emitActivity(observer, ProjectToolActivity(call, ActivityRequested))
		}
		if pending, ok, err := l.requestApproval(ctx, handle, actor, current, completion, round, observer); err != nil {
			return LoopResult{}, err
		} else if ok {
			return LoopResult{Pending: pending, Messages: append([]Message(nil), pending.continuation.Messages...)}, nil
		}

		results := make([]ToolResult, 0, len(completion.ToolCalls))
		for _, call := range completion.ToolCalls {
			activity := ProjectToolActivity(call, ActivityRunning)
			emitActivity(observer, activity)
			tool, exists := l.tools[call.Name]
			if !exists {
				result := failedToolResult(call, "unknown tool")
				emitActivity(observer, completeActivity(activity, result, ActivityFailed))
				results = append(results, result)
				if err := l.recordToolResult(ctx, handle, call.Name, result); err != nil {
					return LoopResult{Messages: toolConversation(current.Messages, completion, results)}, fmt.Errorf("record unknown tool result: %w", err)
				}
				return LoopResult{Messages: toolConversation(current.Messages, completion, results)}, fmt.Errorf("%w: %s", ErrUnknownTool, call.Name)
			}
			result, toolErr := tool.Execute(ctx, handle, call.Name, call.Arguments)
			result.CallID = call.ID
			result.Name = call.Name
			phase := ActivityCompleted
			if toolErr != nil {
				phase = ActivityFailed
			}
			emitActivity(observer, completeActivity(activity, result, phase))
			results = append(results, result)
			if err := l.recordToolResult(ctx, handle, call.Name, result); err != nil {
				return LoopResult{Messages: toolConversation(current.Messages, completion, results)}, fmt.Errorf("record tool result: %w", err)
			}
			if toolErr != nil {
				return LoopResult{Messages: toolConversation(current.Messages, completion, results)}, fmt.Errorf("execute tool %q: %w", call.Name, toolErr)
			}
		}
		current.Messages = toolConversation(current.Messages, completion, results)
	}
}

func toolDefinitionAvailable(tools []ToolDefinition, name string) bool {
	if len(tools) == 0 {
		return true
	}
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func (l *Loop) recordToolResult(ctx context.Context, handle *session.Handle, actor string, result ToolResult) error {
	outcomeCtx, cancelOutcome := session.OutcomeContext(ctx)
	defer cancelOutcome()
	return l.turns.RecordToolResult(outcomeCtx, handle, actor, result)
}

func toolConversation(messages []Message, completion Completion, results []ToolResult) []Message {
	conversation := append([]Message(nil), messages...)
	conversation = append(conversation, Message{Role: RoleAssistant, Content: completion.Content, ToolCalls: completion.ToolCalls})
	conversation = append(conversation, Message{Role: RoleUser, ToolResults: append([]ToolResult(nil), results...)})
	return conversation
}

func (l *Loop) requestApproval(ctx context.Context, handle *session.Handle, actor string, request CompletionRequest, completion Completion, round int, observer ActivityObserver) (*PendingApproval, bool, error) {
	for approvalIndex, call := range completion.ToolCalls {
		tool, exists := l.tools[call.Name]
		if !exists {
			continue
		}
		approvalTool, needsApproval := tool.(ApprovalTool)
		if !needsApproval {
			continue
		}
		pending, err := approvalTool.RequestApproval(ctx, handle, actor, call.Arguments)
		if err != nil {
			emitActivity(observer, ProjectToolActivity(call, ActivityFailed))
			return nil, false, fmt.Errorf("request approval for tool %q: %w", call.Name, err)
		}
		pending.ToolCallID = call.ID
		pending.sessionID = handle.SessionID
		pending.activity = ProjectToolActivity(call, ActivityAwaitingApproval)
		emitActivity(observer, pending.activity)
		pending.continuation = request
		pending.continuation.Messages = append(pending.continuation.Messages, Message{Role: RoleAssistant, Content: completion.Content, ToolCalls: completion.ToolCalls})
		results := make([]ToolResult, 0, len(completion.ToolCalls)-1)
		for index, sibling := range completion.ToolCalls {
			if index == approvalIndex {
				continue
			}
			activity := ProjectToolActivity(sibling, ActivityRunning)
			emitActivity(observer, activity)
			siblingTool, exists := l.tools[sibling.Name]
			if !exists {
				result := failedToolResult(sibling, "unknown tool")
				emitActivity(observer, completeActivity(activity, result, ActivityFailed))
				results = append(results, result)
				if err := l.recordToolResult(ctx, handle, sibling.Name, result); err != nil {
					pending.continuation.Messages = appendPendingResults(pending.continuation.Messages, results)
					return nil, false, fmt.Errorf("record unknown tool result: %w", err)
				}
				continue
			}
			if _, needsApproval := siblingTool.(ApprovalTool); needsApproval {
				result := failedToolResult(sibling, "action deferred until the current approval is resolved")
				emitActivity(observer, completeActivity(activity, result, ActivityFailed))
				results = append(results, result)
				if err := l.recordToolResult(ctx, handle, sibling.Name, result); err != nil {
					pending.continuation.Messages = appendPendingResults(pending.continuation.Messages, results)
					return nil, false, fmt.Errorf("record deferred tool result: %w", err)
				}
				continue
			}
			result, toolErr := siblingTool.Execute(ctx, handle, sibling.Name, sibling.Arguments)
			result.CallID, result.Name = sibling.ID, sibling.Name
			phase := ActivityCompleted
			if toolErr != nil {
				phase = ActivityFailed
			}
			emitActivity(observer, completeActivity(activity, result, phase))
			results = append(results, result)
			if err := l.recordToolResult(ctx, handle, sibling.Name, result); err != nil {
				pending.continuation.Messages = appendPendingResults(pending.continuation.Messages, results)
				return nil, false, fmt.Errorf("record tool result: %w", err)
			}
			if toolErr != nil {
				pending.continuation.Messages = appendPendingResults(pending.continuation.Messages, results)
				return nil, false, fmt.Errorf("execute tool %q: %w", sibling.Name, toolErr)
			}
		}
		if len(results) > 0 {
			pending.continuation.Messages = appendPendingResults(pending.continuation.Messages, results)
		}
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

func appendPendingResults(messages []Message, results []ToolResult) []Message {
	if len(results) == 0 {
		return messages
	}
	return append(messages, Message{Role: RoleUser, ToolResults: append([]ToolResult(nil), results...)})
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

// Used reports whether the approval decision has already been persisted.
func (p *PendingApproval) Used() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.used
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

// RunCommandTool stages a structured workspace command for explicit operator approval.
type RunCommandTool struct {
	workspace *workspace.Service
}

func NewRunCommandTool(workspaceService *workspace.Service) (*RunCommandTool, error) {
	if workspaceService == nil {
		return nil, fmt.Errorf("workspace service is required")
	}
	return &RunCommandTool{workspace: workspaceService}, nil
}

func (t *RunCommandTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "run_command",
		Description: "Run a structured workspace command after operator approval.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"executable":{"type":"string"},"arguments":{"type":"array","items":{"type":"string"}},"working_directory":{"type":"string"}},"required":["executable","arguments"],"additionalProperties":false}`),
	}
}

// Execute is unreachable during normal loop operation because commands always pause first.
func (t *RunCommandTool) Execute(_ context.Context, _ *session.Handle, _ string, _ json.RawMessage) (ToolResult, error) {
	return ToolResult{}, ErrApprovalPending
}

func (t *RunCommandTool) RequestApproval(ctx context.Context, handle *session.Handle, actor string, arguments json.RawMessage) (*PendingApproval, error) {
	var input struct {
		Executable       string   `json:"executable"`
		Arguments        []string `json:"arguments"`
		WorkingDirectory string   `json:"working_directory"`
	}
	if err := json.Unmarshal(arguments, &input); err != nil || input.Executable == "" || input.Arguments == nil {
		return nil, errors.New("invalid run_command arguments")
	}
	command := workspace.Command{
		Executable:       input.Executable,
		Arguments:        input.Arguments,
		WorkingDirectory: input.WorkingDirectory,
	}
	request, err := t.workspace.RequestCommand(ctx, handle, actor, command)
	if err != nil {
		return nil, err
	}
	return &PendingApproval{
		OperationID: request.OperationID.String(),
		Action:      "run_command",
		Summary:     fmt.Sprintf("run %s (%d arguments)", command.Executable, len(command.Arguments)),
		Hash:        request.Hash(),
		approve: func(ctx context.Context, handle *session.Handle, actor string) (ToolResult, error) {
			if err := t.workspace.ApproveCommand(ctx, handle, actor, request); err != nil {
				return ToolResult{}, err
			}
			result, err := t.workspace.ExecuteCommand(ctx, handle, actor, request, command)
			if err != nil {
				toolResult := commandToolResult(result)
				toolResult.IsError = true
				return toolResult, err
			}
			return commandToolResult(result), nil
		},
	}, nil
}

func commandToolResult(result workspace.CommandResult) ToolResult {
	content := string(result.Stdout)
	if len(result.Stderr) > 0 {
		if content != "" {
			content += "\n"
		}
		content += string(result.Stderr)
	}
	if content == "" {
		content = fmt.Sprintf("command completed with exit code %d", result.ExitCode)
	}
	exitCode := result.ExitCode
	return ToolResult{
		Content:   content,
		Bytes:     len(result.Stdout) + len(result.Stderr),
		Hash:      events.Hash([]byte(content)),
		Truncated: result.Truncated,
		ExitCode:  &exitCode,
	}
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
