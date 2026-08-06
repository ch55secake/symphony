package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ch55secake/symphony/internal/audit"
	"github.com/ch55secake/symphony/internal/events"
	"github.com/ch55secake/symphony/internal/session"
	"github.com/ch55secake/symphony/internal/workspace"
)

type sequentialProvider struct {
	completions []Completion
	calls       []CompletionRequest
}

func (p *sequentialProvider) Name() string {
	return "fake"
}

func (p *sequentialProvider) Complete(_ context.Context, request CompletionRequest) (Completion, error) {
	p.calls = append(p.calls, request)
	if len(p.completions) == 0 {
		return Completion{}, errors.New("unexpected completion")
	}
	completion := p.completions[0]
	p.completions = p.completions[1:]
	return completion, nil
}

func TestLoopReadsFileAndContinuesTurn(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	content := "read-only tool content"
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte(content), 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	store := &recordingStore{}
	sessions := session.New(store, audit.DefaultPolicy())
	handle, err := sessions.Start(context.Background(), "user", root)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	turns, err := New(sessions)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	workspaceService, err := workspace.New(sessions, root)
	if err != nil {
		t.Fatalf("workspace.New() error = %v", err)
	}
	readTool, err := NewReadFileTool(workspaceService, 1024)
	if err != nil {
		t.Fatalf("NewReadFileTool() error = %v", err)
	}
	loop, err := NewLoop(turns, []Tool{readTool}, 2)
	if err != nil {
		t.Fatalf("NewLoop() error = %v", err)
	}
	provider := &sequentialProvider{completions: []Completion{
		{ToolCalls: []events.ModelToolCall{{ID: "call-1", Name: "read_file", Arguments: json.RawMessage(`{"path":"note.txt"}`)}}, StopReason: "tool_use"},
		{Content: "The file was read.", StopReason: "stop"},
	}}
	completion, err := loop.Run(context.Background(), handle, "user", provider, CompletionRequest{
		Model:    "test-model",
		Messages: []Message{{Role: RoleUser, Content: "Read note.txt"}},
		Tools:    []ToolDefinition{readTool.Definition()},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if completion.Content != "The file was read." || len(provider.calls) != 2 {
		t.Fatalf("completion = %#v, provider calls = %d", completion, len(provider.calls))
	}
	followUp := provider.calls[1]
	if len(followUp.Messages) != 3 || len(followUp.Messages[1].ToolCalls) != 1 || len(followUp.Messages[2].ToolResults) != 1 || followUp.Messages[2].ToolResults[0].Content != content {
		t.Fatalf("follow-up messages = %#v, want assistant tool call and user result", followUp.Messages)
	}
	if len(store.events) != 9 || store.events[6].Type != events.ToolResult || store.events[7].Type != events.ModelRequested || store.events[8].Type != events.ModelCompleted {
		t.Fatalf("events = %#v, want audited tool loop", store.events)
	}
	encoded, err := json.Marshal(store.events)
	if err != nil {
		t.Fatalf("marshal events: %v", err)
	}
	if strings.Contains(string(encoded), content) {
		t.Fatal("persisted events contain raw file content")
	}
}

func TestLoopRecordsUnknownToolAndStops(t *testing.T) {
	t.Parallel()
	store := &recordingStore{}
	sessions := session.New(store, audit.DefaultPolicy())
	handle, err := sessions.Start(context.Background(), "user", "/workspace")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	turns, err := New(sessions)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	loop, err := NewLoop(turns, nil, 1)
	if err != nil {
		t.Fatalf("NewLoop() error = %v", err)
	}
	provider := &sequentialProvider{completions: []Completion{{ToolCalls: []events.ModelToolCall{{ID: "call-1", Name: "missing", Arguments: json.RawMessage(`{}`)}}}}}
	_, err = loop.Run(context.Background(), handle, "user", provider, CompletionRequest{Model: "test-model", Messages: []Message{{Role: RoleUser, Content: "hello"}}})
	if !errors.Is(err, ErrUnknownTool) {
		t.Fatalf("Run() error = %v, want ErrUnknownTool", err)
	}
	if len(store.events) != 5 || store.events[4].Type != events.ToolResult {
		t.Fatalf("events = %#v, want persisted unknown tool result", store.events)
	}
}

func TestLoopStopsAtConfiguredToolRoundLimit(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("content"), 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	store := &recordingStore{}
	sessions := session.New(store, audit.DefaultPolicy())
	handle, err := sessions.Start(context.Background(), "user", root)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	turns, err := New(sessions)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	workspaceService, err := workspace.New(sessions, root)
	if err != nil {
		t.Fatalf("workspace.New() error = %v", err)
	}
	readTool, err := NewReadFileTool(workspaceService, 1024)
	if err != nil {
		t.Fatalf("NewReadFileTool() error = %v", err)
	}
	loop, err := NewLoop(turns, []Tool{readTool}, 1)
	if err != nil {
		t.Fatalf("NewLoop() error = %v", err)
	}
	call := events.ModelToolCall{ID: "call-1", Name: "read_file", Arguments: json.RawMessage(`{"path":"note.txt"}`)}
	provider := &sequentialProvider{completions: []Completion{{ToolCalls: []events.ModelToolCall{call}}, {ToolCalls: []events.ModelToolCall{call}}}}
	_, err = loop.Run(context.Background(), handle, "user", provider, CompletionRequest{Model: "test-model", Messages: []Message{{Role: RoleUser, Content: "read"}}})
	if !errors.Is(err, ErrMaxToolRounds) {
		t.Fatalf("Run() error = %v, want ErrMaxToolRounds", err)
	}
	if len(provider.calls) != 2 {
		t.Fatalf("provider calls = %d, want 2", len(provider.calls))
	}
}

func TestReadFileToolValidatesArgumentsAndTruncates(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("12345"), 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	store := &recordingStore{}
	sessions := session.New(store, audit.DefaultPolicy())
	handle, err := sessions.Start(context.Background(), "user", root)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	workspaceService, err := workspace.New(sessions, root)
	if err != nil {
		t.Fatalf("workspace.New() error = %v", err)
	}
	readTool, err := NewReadFileTool(workspaceService, 3)
	if err != nil {
		t.Fatalf("NewReadFileTool() error = %v", err)
	}
	result, err := readTool.Execute(context.Background(), handle, "agent", json.RawMessage(`{"path":"note.txt"}`))
	if err != nil || result.Content != "123" || !result.Truncated || result.Bytes != 5 || result.Hash != events.Hash([]byte("12345")) {
		t.Fatalf("Execute() result = %#v, error = %v", result, err)
	}
	if _, err := readTool.Execute(context.Background(), handle, "agent", json.RawMessage(`{"path":false}`)); err == nil {
		t.Fatal("Execute() error = nil, want invalid argument error")
	}
}

func TestWriteFileToolPausesThenApprovesAndResumes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store := &recordingStore{}
	sessions := session.New(store, audit.DefaultPolicy())
	handle, err := sessions.Start(context.Background(), "user", root)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	turns, err := New(sessions)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	workspaceService, err := workspace.New(sessions, root)
	if err != nil {
		t.Fatalf("workspace.New() error = %v", err)
	}
	writeTool, err := NewWriteFileTool(workspaceService)
	if err != nil {
		t.Fatalf("NewWriteFileTool() error = %v", err)
	}
	loop, err := NewLoop(turns, []Tool{writeTool}, 2)
	if err != nil {
		t.Fatalf("NewLoop() error = %v", err)
	}
	provider := &sequentialProvider{completions: []Completion{
		{ToolCalls: []events.ModelToolCall{{ID: "call-1", Name: "write_file", Arguments: json.RawMessage(`{"path":"note.txt","content":"new content"}`)}}, StopReason: "tool_use"},
		{Content: "write complete", StopReason: "stop"},
	}}
	result, err := loop.RunWithApproval(context.Background(), handle, "user", provider, CompletionRequest{Model: "test-model", Messages: []Message{{Role: RoleUser, Content: "write note"}}, Tools: []ToolDefinition{writeTool.Definition()}})
	if err != nil || result.Pending == nil {
		t.Fatalf("RunWithApproval() result = %#v, error = %v; want pending approval", result, err)
	}
	if result.Pending.Action != "write_file" || result.Pending.Summary != "write note.txt (11 bytes)" {
		t.Fatalf("pending = %#v, want safe write metadata", result.Pending)
	}
	if _, err := os.Stat(filepath.Join(root, "note.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file exists before approval: %v", err)
	}
	if len(store.events) != 6 || store.events[4].Type != events.FileWriteRequested || store.events[5].Type != events.ApprovalRequested {
		t.Fatalf("events = %#v, want write request and approval request", store.events)
	}

	resumed, err := loop.Approve(context.Background(), handle, "user", provider, result.Pending)
	if err != nil || resumed.Completion == nil || resumed.Completion.Content != "write complete" {
		t.Fatalf("Approve() result = %#v, error = %v", resumed, err)
	}
	if content, err := os.ReadFile(filepath.Join(root, "note.txt")); err != nil || string(content) != "new content" {
		t.Fatalf("written file = %q, %v", content, err)
	}
	if len(store.events) != 12 || store.events[6].Type != events.ApprovalGranted || store.events[7].Type != events.FileWriteApproved || store.events[8].Type != events.FileWriteCompleted || store.events[9].Type != events.ToolResult {
		t.Fatalf("events = %#v, want approved write lifecycle", store.events)
	}
}

func TestWriteFileToolDenialResumesWithErrorResult(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store := &recordingStore{}
	sessions := session.New(store, audit.DefaultPolicy())
	handle, err := sessions.Start(context.Background(), "user", root)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	turns, err := New(sessions)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	workspaceService, err := workspace.New(sessions, root)
	if err != nil {
		t.Fatalf("workspace.New() error = %v", err)
	}
	writeTool, err := NewWriteFileTool(workspaceService)
	if err != nil {
		t.Fatalf("NewWriteFileTool() error = %v", err)
	}
	loop, err := NewLoop(turns, []Tool{writeTool}, 2)
	if err != nil {
		t.Fatalf("NewLoop() error = %v", err)
	}
	provider := &sequentialProvider{completions: []Completion{
		{ToolCalls: []events.ModelToolCall{{ID: "call-1", Name: "write_file", Arguments: json.RawMessage(`{"path":"note.txt","content":"new content"}`)}}, StopReason: "tool_use"},
		{Content: "write denied", StopReason: "stop"},
	}}
	result, err := loop.RunWithApproval(context.Background(), handle, "user", provider, CompletionRequest{Model: "test-model", Messages: []Message{{Role: RoleUser, Content: "write note"}}})
	if err != nil || result.Pending == nil {
		t.Fatalf("RunWithApproval() result = %#v, error = %v", result, err)
	}
	resumed, err := loop.Deny(context.Background(), handle, "user", provider, result.Pending, "user_denied")
	if err != nil || resumed.Completion == nil || resumed.Completion.Content != "write denied" {
		t.Fatalf("Deny() result = %#v, error = %v", resumed, err)
	}
	if _, err := os.Stat(filepath.Join(root, "note.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file exists after denial: %v", err)
	}
	if len(provider.calls) != 2 || len(provider.calls[1].Messages[2].ToolResults) != 1 || !provider.calls[1].Messages[2].ToolResults[0].IsError {
		t.Fatalf("follow-up = %#v, want error tool result", provider.calls)
	}
	if len(store.events) != 10 || store.events[6].Type != events.ApprovalDenied || store.events[7].Type != events.ToolResult {
		t.Fatalf("events = %#v, want denied approval lifecycle", store.events)
	}
}

func TestRunCommandToolPausesThenApprovesAndResumes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store := &recordingStore{}
	sessions := session.New(store, audit.DefaultPolicy())
	handle, err := sessions.Start(context.Background(), "user", root)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	turns, err := New(sessions)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	workspaceService, err := workspace.New(sessions, root)
	if err != nil {
		t.Fatalf("workspace.New() error = %v", err)
	}
	commandTool, err := NewRunCommandTool(workspaceService)
	if err != nil {
		t.Fatalf("NewRunCommandTool() error = %v", err)
	}
	loop, err := NewLoop(turns, []Tool{commandTool}, 2)
	if err != nil {
		t.Fatalf("NewLoop() error = %v", err)
	}
	provider := &sequentialProvider{completions: []Completion{
		{ToolCalls: []events.ModelToolCall{{ID: "call-1", Name: "run_command", Arguments: json.RawMessage(`{"executable":"sh","arguments":["-c","printf command-output"]}`)}}, StopReason: "tool_use"},
		{Content: "command complete", StopReason: "stop"},
	}}
	result, err := loop.RunWithApproval(context.Background(), handle, "user", provider, CompletionRequest{Model: "test-model", Messages: []Message{{Role: RoleUser, Content: "run command"}}, Tools: []ToolDefinition{commandTool.Definition()}})
	if err != nil || result.Pending == nil {
		t.Fatalf("RunWithApproval() result = %#v, error = %v; want pending approval", result, err)
	}
	if result.Pending.Action != "run_command" || result.Pending.Summary != "run sh (2 arguments)" {
		t.Fatalf("pending = %#v, want safe command metadata", result.Pending)
	}
	if len(store.events) != 6 || store.events[4].Type != events.CommandRequested || store.events[5].Type != events.ApprovalRequested {
		t.Fatalf("events = %#v, want command request and approval request", store.events)
	}

	resumed, err := loop.Approve(context.Background(), handle, "user", provider, result.Pending)
	if err != nil || resumed.Completion == nil || resumed.Completion.Content != "command complete" {
		t.Fatalf("Approve() result = %#v, error = %v", resumed, err)
	}
	if len(provider.calls) != 2 || provider.calls[1].Messages[2].ToolResults[0].CallID != "call-1" || provider.calls[1].Messages[2].ToolResults[0].Name != "run_command" || provider.calls[1].Messages[2].ToolResults[0].Content != "command-output" {
		t.Fatalf("follow-up = %#v, want command output tool result", provider.calls)
	}
	if len(store.events) != 12 || store.events[6].Type != events.ApprovalGranted || store.events[7].Type != events.CommandApproved || store.events[8].Type != events.CommandCompleted || store.events[9].Type != events.ToolResult {
		t.Fatalf("events = %#v, want approved command lifecycle", store.events)
	}
}

func TestRunCommandToolDenialResumesWithoutExecution(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store := &recordingStore{}
	sessions := session.New(store, audit.DefaultPolicy())
	handle, err := sessions.Start(context.Background(), "user", root)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	turns, _ := New(sessions)
	workspaceService, _ := workspace.New(sessions, root)
	commandTool, _ := NewRunCommandTool(workspaceService)
	loop, _ := NewLoop(turns, []Tool{commandTool}, 2)
	provider := &sequentialProvider{completions: []Completion{
		{ToolCalls: []events.ModelToolCall{{ID: "call-1", Name: "run_command", Arguments: json.RawMessage(`{"executable":"sh","arguments":["-c","touch marker"]}`)}}},
		{Content: "command denied"},
	}}
	result, err := loop.RunWithApproval(context.Background(), handle, "user", provider, CompletionRequest{Model: "test-model", Messages: []Message{{Role: RoleUser, Content: "run command"}}})
	if err != nil || result.Pending == nil {
		t.Fatalf("RunWithApproval() result = %#v, error = %v", result, err)
	}
	resumed, err := loop.Deny(context.Background(), handle, "user", provider, result.Pending, "user_denied")
	if err != nil || resumed.Completion == nil || resumed.Completion.Content != "command denied" {
		t.Fatalf("Deny() result = %#v, error = %v", resumed, err)
	}
	if _, err := os.Stat(filepath.Join(root, "marker")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("command executed after denial: %v", err)
	}
	if len(store.events) != 10 || store.events[6].Type != events.ApprovalDenied || store.events[7].Type != events.ToolResult {
		t.Fatalf("events = %#v, want denied command lifecycle", store.events)
	}
}

func TestRunCommandToolValidatesArgumentsAndRedactsOutput(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store := &recordingStore{}
	sessions := session.New(store, audit.DefaultPolicy())
	handle, err := sessions.Start(context.Background(), "user", root)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	workspaceService, err := workspace.New(sessions, root)
	if err != nil {
		t.Fatalf("workspace.New() error = %v", err)
	}
	commandTool, err := NewRunCommandTool(workspaceService)
	if err != nil {
		t.Fatalf("NewRunCommandTool() error = %v", err)
	}
	if _, err := commandTool.RequestApproval(context.Background(), handle, "agent", json.RawMessage(`{"executable":"sh"}`)); err == nil {
		t.Fatal("RequestApproval() error = nil, want invalid argument error")
	}
	secret := "Bearer top-secret"
	pending, err := commandTool.RequestApproval(context.Background(), handle, "agent", json.RawMessage(`{"executable":"sh","arguments":["-c","printf %s \"$1\"","sh","Bearer top-secret","--token=abc123"]}`))
	if err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}
	result, err := pending.approve(context.Background(), handle, "user")
	if err != nil {
		t.Fatalf("approved command error = %v", err)
	}
	if result.Content != secret {
		t.Fatalf("command result = %#v, want runtime-only command output", result)
	}
	encoded, err := json.Marshal(store.events)
	if err != nil {
		t.Fatalf("marshal events: %v", err)
	}
	if strings.Contains(string(encoded), secret) || strings.Contains(string(encoded), "--token=abc123") {
		t.Fatal("persisted events contain raw secret command data or output")
	}
}

func TestPendingApprovalRejectsCrossSessionAndReuse(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store := &recordingStore{}
	sessions := session.New(store, audit.DefaultPolicy())
	handle, err := sessions.Start(context.Background(), "user", root)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	other, err := sessions.Start(context.Background(), "user", root)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	turns, _ := New(sessions)
	workspaceService, _ := workspace.New(sessions, root)
	writeTool, _ := NewWriteFileTool(workspaceService)
	loop, _ := NewLoop(turns, []Tool{writeTool}, 1)
	provider := &sequentialProvider{completions: []Completion{{ToolCalls: []events.ModelToolCall{{ID: "call-1", Name: "write_file", Arguments: json.RawMessage(`{"path":"note.txt","content":"new"}`)}}}, {Content: "done"}}}
	result, err := loop.RunWithApproval(context.Background(), handle, "user", provider, CompletionRequest{Model: "test-model", Messages: []Message{{Role: RoleUser, Content: "write"}}})
	if err != nil {
		t.Fatalf("RunWithApproval() error = %v", err)
	}
	if _, err := loop.Approve(context.Background(), other, "user", provider, result.Pending); !errors.Is(err, ErrApprovalSession) {
		t.Fatalf("Approve() error = %v, want ErrApprovalSession", err)
	}
	if _, err := loop.Approve(context.Background(), handle, "user", provider, result.Pending); err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if _, err := loop.Approve(context.Background(), handle, "user", provider, result.Pending); !errors.Is(err, ErrApprovalUsed) {
		t.Fatalf("Approve() error = %v, want ErrApprovalUsed", err)
	}
}
