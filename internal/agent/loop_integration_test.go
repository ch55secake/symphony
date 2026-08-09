package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ch55secake/symphony/internal/agent"
	"github.com/ch55secake/symphony/internal/audit"
	"github.com/ch55secake/symphony/internal/events"
	"github.com/ch55secake/symphony/internal/session"
	"github.com/ch55secake/symphony/internal/store/kurrentdb"
	"github.com/ch55secake/symphony/internal/workspace"
)

type loopProvider struct {
	completions []agent.Completion
}

func (p *loopProvider) Name() string {
	return "fake"
}

func (p *loopProvider) Complete(_ context.Context, _ agent.CompletionRequest) (agent.Completion, error) {
	if len(p.completions) == 0 {
		return agent.Completion{}, errors.New("unexpected completion")
	}
	completion := p.completions[0]
	p.completions = p.completions[1:]
	return completion, nil
}

func TestLoopPersistsReadOnlyToolSequence(t *testing.T) {
	connectionString := os.Getenv("KURRENTDB_URL")
	if connectionString == "" {
		t.Skip("KURRENTDB_URL is not set")
	}
	store, err := kurrentdb.New(connectionString)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("content"), 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	sessions := session.New(store, audit.DefaultPolicy())
	handle, err := sessions.Start(ctx, "user", root)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	turns, err := agent.New(sessions)
	if err != nil {
		t.Fatalf("agent.New() error = %v", err)
	}
	workspaceService, err := workspace.New(sessions, root)
	if err != nil {
		t.Fatalf("workspace.New() error = %v", err)
	}
	readTool, err := agent.NewReadFileTool(workspaceService, 1024)
	if err != nil {
		t.Fatalf("NewReadFileTool() error = %v", err)
	}
	loop, err := agent.NewLoop(turns, []agent.Tool{readTool}, 1)
	if err != nil {
		t.Fatalf("NewLoop() error = %v", err)
	}
	provider := &loopProvider{completions: []agent.Completion{
		{ToolCalls: []events.ModelToolCall{{ID: "call-1", Name: "read_file", Arguments: json.RawMessage(`{"path":"note.txt"}`)}}, StopReason: "tool_use"},
		{Content: "done", StopReason: "stop"},
	}}
	if _, err := loop.Run(ctx, handle, "user", provider, agent.CompletionRequest{
		Model:    "test-model",
		Messages: []agent.Message{{Role: agent.RoleUser, Content: "Read note.txt"}},
		Tools:    []agent.ToolDefinition{readTool.Definition()},
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	persisted, err := store.Read(ctx, handle.SessionID)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(persisted) != 9 || persisted[6].Type != events.ToolResultV2 || persisted[7].Type != events.ModelRequested || persisted[8].Type != events.ModelCompletedV2 {
		t.Fatalf("persisted events = %#v, want complete read-only tool loop", persisted)
	}
}

func TestLoopPersistsApprovedWriteSequence(t *testing.T) {
	connectionString := os.Getenv("KURRENTDB_URL")
	if connectionString == "" {
		t.Skip("KURRENTDB_URL is not set")
	}
	store, err := kurrentdb.New(connectionString)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	root := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	sessions := session.New(store, audit.DefaultPolicy())
	handle, err := sessions.Start(ctx, "user", root)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	turns, err := agent.New(sessions)
	if err != nil {
		t.Fatalf("agent.New() error = %v", err)
	}
	workspaceService, err := workspace.New(sessions, root)
	if err != nil {
		t.Fatalf("workspace.New() error = %v", err)
	}
	writeTool, err := agent.NewWriteFileTool(workspaceService)
	if err != nil {
		t.Fatalf("NewWriteFileTool() error = %v", err)
	}
	loop, err := agent.NewLoop(turns, []agent.Tool{writeTool}, 1)
	if err != nil {
		t.Fatalf("NewLoop() error = %v", err)
	}
	provider := &loopProvider{completions: []agent.Completion{
		{ToolCalls: []events.ModelToolCall{{ID: "call-1", Name: "write_file", Arguments: json.RawMessage(`{"path":"note.txt","content":"content"}`)}}, StopReason: "tool_use"},
		{Content: "done", StopReason: "stop"},
	}}
	paused, err := loop.RunWithApproval(ctx, handle, "user", provider, agent.CompletionRequest{Model: "test-model", Messages: []agent.Message{{Role: agent.RoleUser, Content: "Write note.txt"}}})
	if err != nil || paused.Pending == nil {
		t.Fatalf("RunWithApproval() result = %#v, error = %v", paused, err)
	}
	if _, err := loop.Approve(ctx, handle, "user", provider, paused.Pending); err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if content, err := os.ReadFile(filepath.Join(root, "note.txt")); err != nil || string(content) != "content" {
		t.Fatalf("written file = %q, %v", content, err)
	}

	persisted, err := store.Read(ctx, handle.SessionID)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(persisted) != 12 || persisted[5].Type != events.ApprovalRequested || persisted[6].Type != events.ApprovalGranted || persisted[8].Type != events.FileWriteCompleted || persisted[9].Type != events.ToolResultV2 {
		t.Fatalf("persisted events = %#v, want complete approved write sequence", persisted)
	}
}

func TestLoopPersistsApprovedCommandSequence(t *testing.T) {
	connectionString := os.Getenv("KURRENTDB_URL")
	if connectionString == "" {
		t.Skip("KURRENTDB_URL is not set")
	}
	store, err := kurrentdb.New(connectionString)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	root := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	sessions := session.New(store, audit.DefaultPolicy())
	handle, err := sessions.Start(ctx, "user", root)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	turns, err := agent.New(sessions)
	if err != nil {
		t.Fatalf("agent.New() error = %v", err)
	}
	workspaceService, err := workspace.New(sessions, root)
	if err != nil {
		t.Fatalf("workspace.New() error = %v", err)
	}
	commandTool, err := agent.NewRunCommandTool(workspaceService)
	if err != nil {
		t.Fatalf("NewRunCommandTool() error = %v", err)
	}
	loop, err := agent.NewLoop(turns, []agent.Tool{commandTool}, 1)
	if err != nil {
		t.Fatalf("NewLoop() error = %v", err)
	}
	provider := &loopProvider{completions: []agent.Completion{
		{ToolCalls: []events.ModelToolCall{{ID: "call-1", Name: "run_command", Arguments: json.RawMessage(`{"executable":"sh","arguments":["-c","printf content"]}`)}}, StopReason: "tool_use"},
		{Content: "done", StopReason: "stop"},
	}}
	paused, err := loop.RunWithApproval(ctx, handle, "user", provider, agent.CompletionRequest{Model: "test-model", Messages: []agent.Message{{Role: agent.RoleUser, Content: "Run command"}}})
	if err != nil || paused.Pending == nil {
		t.Fatalf("RunWithApproval() result = %#v, error = %v", paused, err)
	}
	if _, err := loop.Approve(ctx, handle, "user", provider, paused.Pending); err != nil {
		t.Fatalf("Approve() error = %v", err)
	}

	persisted, err := store.Read(ctx, handle.SessionID)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(persisted) != 12 || persisted[4].Type != events.CommandRequested || persisted[5].Type != events.ApprovalRequested || persisted[6].Type != events.ApprovalGranted || persisted[8].Type != events.CommandCompleted || persisted[9].Type != events.ToolResultV2 {
		t.Fatalf("persisted events = %#v, want complete approved command sequence", persisted)
	}
}
