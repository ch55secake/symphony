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
