package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ch55secake/symphony/internal/agent"
	"github.com/ch55secake/symphony/internal/audit"
	appconfig "github.com/ch55secake/symphony/internal/config"
	"github.com/ch55secake/symphony/internal/events"
	"github.com/ch55secake/symphony/internal/session"
	"github.com/ch55secake/symphony/internal/tui"
	"github.com/ch55secake/symphony/internal/workspace"
	"github.com/google/uuid"
)

type memoryStore struct {
	events []events.Event
}

func (s *memoryStore) Append(_ context.Context, event events.Event, _ *uint64) (uint64, error) {
	event.Sequence = uint64(len(s.events))
	s.events = append(s.events, event)
	return event.Sequence, nil
}

type fakeProvider struct {
	completions []agent.Completion
	err         error
}

type fakeReplayReader struct {
	events    []events.Event
	err       error
	sessionID uuid.UUID
	closed    bool
}

func (r *fakeReplayReader) Read(_ context.Context, sessionID uuid.UUID) ([]events.Event, error) {
	r.sessionID = sessionID
	return r.events, r.err
}

func (r *fakeReplayReader) Close() error {
	r.closed = true
	return nil
}

func (p *fakeProvider) Name() string { return "fake" }

func (p *fakeProvider) Complete(_ context.Context, _ agent.CompletionRequest) (agent.Completion, error) {
	if p.err != nil {
		return agent.Completion{}, p.err
	}
	if len(p.completions) == 0 {
		return agent.Completion{}, errors.New("unexpected completion")
	}
	completion := p.completions[0]
	p.completions = p.completions[1:]
	return completion, nil
}

func TestRunCompletesReadOnlySession(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("private content"), 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	provider := &fakeProvider{completions: []agent.Completion{
		{ToolCalls: []events.ModelToolCall{{ID: "call-1", Name: "read_file", Arguments: json.RawMessage(`{"path":"note.txt"}`)}}, StopReason: "tool_use"},
		{Content: "read complete", StopReason: "stop"},
	}}
	factory, store := testRuntime(t, root, provider, func(service *workspace.Service) ([]agent.Tool, error) {
		tool, err := agent.NewReadFileTool(service, 1024)
		return []agent.Tool{tool}, err
	})
	var output strings.Builder
	err := run(context.Background(), []string{"run", "--provider", "openai", "--model", "test", "--workspace", root, "read note"}, strings.NewReader(""), &output, factory)
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if !strings.Contains(output.String(), "Session: ") || !strings.HasSuffix(output.String(), "read complete\n") || store.events[len(store.events)-1].Type != events.SessionFinished {
		t.Fatalf("output = %q, events = %#v", output.String(), store.events)
	}
	encoded, err := json.Marshal(store.events)
	if err != nil {
		t.Fatalf("marshal events: %v", err)
	}
	if strings.Contains(string(encoded), "private content") {
		t.Fatal("persisted events contain raw file content")
	}
}

func TestReplayWritesEventsInOrderWithoutSessionWrites(t *testing.T) {
	sessionID := uuid.New()
	first, err := events.New(sessionID, events.SessionStarted, "cli", uuid.New(), events.SessionStartedPayload{Workspace: "/workspace"}, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	second, err := events.New(sessionID, events.SessionFinished, "cli", uuid.New(), events.SessionFinishedPayload{Reason: "completed"}, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	reader := &fakeReplayReader{events: []events.Event{first, second}}
	var output strings.Builder
	t.Setenv("KURRENTDB_URL", "kurrentdb://localhost:2113?tls=false")
	err = replay(context.Background(), []string{sessionID.String()}, &output, func(connectionString string) (replayReader, error) {
		if connectionString != "kurrentdb://localhost:2113?tls=false" {
			t.Fatalf("connection string = %q", connectionString)
		}
		return reader, nil
	})
	if err != nil || reader.sessionID != sessionID || !reader.closed {
		t.Fatalf("replay() error = %v, reader = %#v", err, reader)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("output = %q, want two JSONL events", output.String())
	}
	var replayed []events.Event
	for _, line := range lines {
		var event events.Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("unmarshal replay event: %v", err)
		}
		replayed = append(replayed, event)
	}
	if replayed[0].ID != first.ID || replayed[1].ID != second.ID {
		t.Fatalf("replayed events = %#v, want original order", replayed)
	}
}

func TestReplayValidatesArgumentsAndReadFailures(t *testing.T) {
	t.Setenv("KURRENTDB_URL", "kurrentdb://localhost:2113?tls=false")
	if err := replay(context.Background(), nil, ioDiscard{}, func(string) (replayReader, error) { return nil, nil }); err == nil {
		t.Fatal("replay() error = nil, want usage error")
	}
	if err := replay(context.Background(), []string{"invalid"}, ioDiscard{}, func(string) (replayReader, error) { return nil, nil }); err == nil {
		t.Fatal("replay() error = nil, want UUID error")
	}
	reader := &fakeReplayReader{err: errors.New("read failed")}
	err := replay(context.Background(), []string{uuid.New().String()}, ioDiscard{}, func(string) (replayReader, error) { return reader, nil })
	if err == nil || !reader.closed {
		t.Fatalf("replay() error = %v, reader = %#v", err, reader)
	}
}

func TestRunApprovesWriteAndFinishesSession(t *testing.T) {
	root := t.TempDir()
	provider := &fakeProvider{completions: []agent.Completion{
		{ToolCalls: []events.ModelToolCall{{ID: "call-1", Name: "write_file", Arguments: json.RawMessage(`{"path":"note.txt","content":"content"}`)}}, StopReason: "tool_use"},
		{Content: "write complete", StopReason: "stop"},
	}}
	factory, store := testRuntime(t, root, provider, func(service *workspace.Service) ([]agent.Tool, error) {
		tool, err := agent.NewWriteFileTool(service)
		return []agent.Tool{tool}, err
	})
	var output strings.Builder
	err := run(context.Background(), []string{"run", "--provider", "openai", "--model", "test", "--workspace", root, "write note"}, strings.NewReader("yes\n"), &output, factory)
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if content, err := os.ReadFile(filepath.Join(root, "note.txt")); err != nil || string(content) != "content" {
		t.Fatalf("written file = %q, %v", content, err)
	}
	if !strings.Contains(output.String(), "Approval required: write note.txt (7 bytes)") || !strings.HasSuffix(output.String(), "write complete\n") {
		t.Fatalf("output = %q", output.String())
	}
	if store.events[len(store.events)-1].Type != events.SessionFinished {
		t.Fatalf("events = %#v, want finished session", store.events)
	}
}

func TestRunDeniesWriteAndFinishesSession(t *testing.T) {
	root := t.TempDir()
	provider := &fakeProvider{completions: []agent.Completion{
		{ToolCalls: []events.ModelToolCall{{ID: "call-1", Name: "write_file", Arguments: json.RawMessage(`{"path":"note.txt","content":"content"}`)}}, StopReason: "tool_use"},
		{Content: "write denied", StopReason: "stop"},
	}}
	factory, store := testRuntime(t, root, provider, func(service *workspace.Service) ([]agent.Tool, error) {
		tool, err := agent.NewWriteFileTool(service)
		return []agent.Tool{tool}, err
	})
	var output strings.Builder
	err := run(context.Background(), []string{"run", "--provider", "openai", "--model", "test", "--workspace", root, "write note"}, strings.NewReader("n\n"), &output, factory)
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "note.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file exists after denial: %v", err)
	}
	if !hasEvent(store.events, events.ApprovalDenied) || hasEvent(store.events, events.FileWriteApproved) || store.events[len(store.events)-1].Type != events.SessionFinished {
		t.Fatalf("events = %#v, want denied and finished lifecycle", store.events)
	}
}

func TestRunFailsSessionWhenProviderFails(t *testing.T) {
	root := t.TempDir()
	factory, store := testRuntime(t, root, &fakeProvider{err: errors.New("provider unavailable")}, func(_ *workspace.Service) ([]agent.Tool, error) {
		return nil, nil
	})
	err := run(context.Background(), []string{"run", "--provider", "openai", "--model", "test", "--workspace", root, "hello"}, strings.NewReader(""), ioDiscard{}, factory)
	if err == nil || store.events[len(store.events)-1].Type != events.SessionFailed {
		t.Fatalf("run() error = %v, events = %#v; want failed session", err, store.events)
	}
}

func TestParseConfigValidatesRunArguments(t *testing.T) {
	root := t.TempDir()
	tests := [][]string{
		{"status"},
		{"run", "--provider", "other", "--model", "test", "hello"},
		{"run", "--provider", "opencode", "--transport", "other", "--model", "test", "hello"},
		{"run", "--provider", "openai", "hello"},
		{"run", "--provider", "openai", "--model", "test"},
	}
	for _, args := range tests {
		if _, err := parseConfig(args); err == nil {
			t.Fatalf("parseConfig(%q) error = nil", args)
		}
	}
	config, err := parseConfig([]string{"run", "--provider", "anthropic", "--model", "test", "--workspace", root, "hello"})
	if err != nil || config.workspace != root || config.prompt != "hello" {
		t.Fatalf("parseConfig() = %#v, %v", config, err)
	}
	config, err = parseConfig([]string{"run", "--provider", "opencode", "--transport", "chat-completions", "--model", "kimi-test", "--workspace", root, "hello"})
	if err != nil || config.transport != "chat-completions" {
		t.Fatalf("parseConfig() = %#v, %v", config, err)
	}
}

func TestConfigFromTUIUsesSelectionAndLocalKurrentDB(t *testing.T) {
	settings := appconfig.Settings{
		Transport:       "responses",
		OpenAIAPIKey:    "configured-key",
		AnthropicAPIKey: "anthropic-key",
	}
	parsed, err := configFromTUI(tui.SetupConfig{Provider: "anthropic", Model: "selected-model", Workspace: "/workspace", APIKey: "selected-key"}, settings)
	if err != nil {
		t.Fatalf("configFromTUI() error = %v", err)
	}
	if parsed.provider != "anthropic" || parsed.model != "selected-model" || parsed.workspace != "/workspace" || parsed.connectionString != localKurrentDBURL || parsed.apiKey != "selected-key" {
		t.Fatalf("configFromTUI() = %#v", parsed)
	}
}

func TestConfigFromTUIValidatesSelection(t *testing.T) {
	for _, selected := range []tui.SetupConfig{
		{Provider: "other", Model: "model", Workspace: "/workspace"},
		{Provider: "openai", Workspace: "/workspace"},
	} {
		if _, err := configFromTUI(selected, appconfig.Settings{}); err == nil {
			t.Fatalf("configFromTUI(%#v) error = nil", selected)
		}
	}
}

func TestParseConfigUsesFileEnvironmentAndFlagPrecedence(t *testing.T) {
	settings := appconfig.Settings{
		Provider:     "openai",
		Model:        "environment-model",
		Transport:    "responses",
		Workspace:    "/file-workspace",
		OpenAIAPIKey: "environment-key",
	}
	parsed, err := parseConfigWithSettings([]string{"run", "--model", "flag-model", "hello"}, settings)
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if parsed.provider != "openai" || parsed.model != "flag-model" || parsed.workspace != "/file-workspace" || parsed.apiKey != "environment-key" {
		t.Fatalf("parseConfig() = %#v", parsed)
	}
}

func TestNewProviderUsesConfiguredOpenCodeAPIKey(t *testing.T) {
	provider, err := newProvider("opencode", "responses", "test-key")
	if err != nil || provider.Name() != "opencode" {
		t.Fatalf("newProvider() = %#v, %v", provider, err)
	}
}

func testRuntime(t *testing.T, root string, provider agent.Provider, setup func(*workspace.Service) ([]agent.Tool, error)) (runtimeFactory, *memoryStore) {
	t.Helper()
	store := &memoryStore{}
	return func(_ config) (*runtime, error) {
		sessions := session.New(store, audit.DefaultPolicy())
		turns, err := agent.New(sessions)
		if err != nil {
			return nil, err
		}
		workspaceService, err := workspace.New(sessions, root)
		if err != nil {
			return nil, err
		}
		tools, err := setup(workspaceService)
		if err != nil {
			return nil, err
		}
		loop, err := agent.NewLoop(turns, tools, 2)
		if err != nil {
			return nil, err
		}
		definitions := make([]agent.ToolDefinition, 0, len(tools))
		for _, tool := range tools {
			definitions = append(definitions, tool.Definition())
		}
		return &runtime{sessions: sessions, loop: loop, provider: provider, tools: definitions, close: func() error { return nil }}, nil
	}, store
}

func hasEvent(events []events.Event, eventType events.Type) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}

type ioDiscard struct{}

func (ioDiscard) Write(data []byte) (int, error) { return len(data), nil }
