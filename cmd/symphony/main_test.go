package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ch55secake/symphony/internal/agent"
	"github.com/ch55secake/symphony/internal/audit"
	appconfig "github.com/ch55secake/symphony/internal/config"
	"github.com/ch55secake/symphony/internal/events"
	"github.com/ch55secake/symphony/internal/providers/opencode"
	"github.com/ch55secake/symphony/internal/session"
	"github.com/ch55secake/symphony/internal/tui"
	"github.com/ch55secake/symphony/internal/ui"
	"github.com/ch55secake/symphony/internal/workspace"
	"github.com/google/uuid"
)

func TestSendUIStateIncludesStructuredToolActivity(t *testing.T) {
	var buffer bytes.Buffer
	exitCode := 0
	activities := []uiToolActivity{{AfterMessages: 1, Activity: agent.ToolActivity{ID: "call-1", Name: "run_command", Phase: agent.ActivityCompleted, Command: "go test ./...", ExitCode: &exitCode, OutputHidden: true}}}
	if err := sendUIState(func(state ui.State) error { return ui.SendState(&buffer, state) }, config{provider: "openai", model: "test-model", workspace: "/workspace"}, []agent.Message{{Role: agent.RoleUser, Content: "test"}}, activities, nil, "READY"); err != nil {
		t.Fatalf("sendUIState() error = %v", err)
	}
	message, err := ui.Read(bufio.NewReader(&buffer))
	if err != nil {
		t.Fatalf("read UI state: %v", err)
	}
	var state ui.State
	if err := json.Unmarshal(message.Payload, &state); err != nil {
		t.Fatalf("decode UI state: %v", err)
	}
	if len(state.Transcript) != 2 || state.Transcript[1].Tool == nil || state.Transcript[1].Tool.Command != "go test ./..." || state.Transcript[1].Tool.ExitCode == nil || *state.Transcript[1].Tool.ExitCode != 0 {
		t.Fatalf("transcript = %#v", state.Transcript)
	}
}

func TestUpsertUIActivityDoesNotOverwriteReusedProviderID(t *testing.T) {
	activities := upsertUIActivity(nil, uiToolActivity{SourceID: "call-1", AfterMessages: 1, Activity: agent.ToolActivity{ID: "call-1", Phase: agent.ActivityRequested}})
	activities = upsertUIActivity(activities, uiToolActivity{SourceID: "call-1", AfterMessages: 1, Activity: agent.ToolActivity{ID: "call-1", Phase: agent.ActivityCompleted}})
	activities = upsertUIActivity(activities, uiToolActivity{SourceID: "call-1", AfterMessages: 3, Activity: agent.ToolActivity{ID: "call-1", Phase: agent.ActivityRequested}})
	messages := []agent.Message{
		{Role: agent.RoleAssistant, ToolCalls: []events.ModelToolCall{{ID: "call-1"}}},
		{Role: agent.RoleAssistant, ToolCalls: []events.ModelToolCall{{ID: "call-1"}}},
	}
	activities = alignUIActivities(messages, activities)
	if len(activities) != 2 || activities[0].Activity.ID == activities[1].Activity.ID || activities[0].AfterMessages != 1 || activities[1].AfterMessages != 2 {
		t.Fatalf("activities = %#v", activities)
	}
}

type memoryStore struct {
	events []events.Event
}

func (s *memoryStore) Append(ctx context.Context, event events.Event, _ *uint64) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	event.Sequence = uint64(len(s.events))
	s.events = append(s.events, event)
	return event.Sequence, nil
}

type fakeProvider struct {
	completions []agent.Completion
	err         error
}

type cancelingProvider struct {
	canceled    chan struct{}
	startedPath string
}

type approvalResumeProvider struct {
	calls   int
	resumed chan struct{}
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

func (p *cancelingProvider) Name() string { return "canceling" }

func (p *cancelingProvider) Complete(ctx context.Context, _ agent.CompletionRequest) (agent.Completion, error) {
	if p.startedPath != "" {
		_ = os.WriteFile(p.startedPath, []byte("started"), 0o600)
	}
	<-ctx.Done()
	close(p.canceled)
	return agent.Completion{}, ctx.Err()
}

func (p *approvalResumeProvider) Name() string { return "approval-resume" }

func (p *approvalResumeProvider) Complete(ctx context.Context, _ agent.CompletionRequest) (agent.Completion, error) {
	p.calls++
	if p.calls == 1 {
		return agent.Completion{ToolCalls: []events.ModelToolCall{{ID: "call-1", Name: "write_file", Arguments: json.RawMessage(`{"path":"note.txt","content":"content"}`)}}, StopReason: "tool_use"}, nil
	}
	close(p.resumed)
	<-ctx.Done()
	return agent.Completion{}, ctx.Err()
}

func TestOpenTUICtrlCCancelsWorkThenQuits(t *testing.T) {
	root := t.TempDir()
	provider := &cancelingProvider{canceled: make(chan struct{}), startedPath: filepath.Join(root, "provider.started")}
	factory, store := testRuntime(t, root, provider, func(_ *workspace.Service) ([]agent.Tool, error) { return nil, nil })
	executable := writeUIFixture(t, `#!/bin/sh
printf '%s\n' '{"version":1,"type":"app.ready"}' >&4
printf '%s\n' '{"version":1,"type":"chat.start"}' >&4
printf '%s\n' '{"version":1,"type":"prompt.submit","payload":{"prompt":"wait"}}' >&4
while IFS= read -r state <&3; do
  case "$state" in *'"status":"WORKING"'*) break ;; esac
done
while [ ! -f "$SYMPHONY_TEST_PROVIDER_STARTED" ]; do :; done
printf '%s\n' '{"version":1,"type":"app.cancel"}' >&4
while IFS= read -r state <&3; do
  case "$state" in *'"status":"Canceled"'*) break ;; esac
done
printf '%s\n' '{"version":1,"type":"app.cancel"}' >&4
while IFS= read -r state <&3; do
  case "$state" in *'"type":"app.shutdown"'*) exit 0 ;; esac
done
`)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PROVIDER", "openai")
	t.Setenv("MODEL", "test-model")
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("SYMPHONY_TEST_PROVIDER_STARTED", provider.startedPath)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runOpenTUI(ctx, factory, func(context.Context) error { return nil }, executable); err != nil {
		t.Fatalf("runOpenTUI() error = %v", err)
	}
	select {
	case <-provider.canceled:
	default:
		t.Fatal("provider request was not canceled")
	}
	if !hasEvent(store.events, events.ModelFailed) || len(store.events) == 0 || store.events[len(store.events)-1].Type != events.SessionFinished {
		t.Fatalf("events = %#v, want finished session", store.events)
	}
}

func TestOpenTUIApprovalDisappearsWhileWorkResumes(t *testing.T) {
	root := t.TempDir()
	provider := &approvalResumeProvider{resumed: make(chan struct{})}
	factory, _ := testRuntime(t, root, provider, func(service *workspace.Service) ([]agent.Tool, error) {
		tool, err := agent.NewWriteFileTool(service)
		return []agent.Tool{tool}, err
	})
	executable := writeUIFixture(t, `#!/bin/sh
printf '%s\n' '{"version":1,"type":"app.ready"}' >&4
printf '%s\n' '{"version":1,"type":"chat.start"}' >&4
printf '%s\n' '{"version":1,"type":"prompt.submit","payload":{"prompt":"write note"}}' >&4
while IFS= read -r state <&3; do
  case "$state" in *'"approval":'*) break ;; esac
done
printf '%s\n' '{"version":1,"type":"approval.resolve","payload":{"approved":true}}' >&4
while IFS= read -r state <&3; do
  case "$state" in
    *'"phase":"completed"'*)
      case "$state" in *'"approval":'*) exit 23 ;; esac
      break
      ;;
  esac
done
printf '%s\n' '{"version":1,"type":"app.quit"}' >&4
while IFS= read -r state <&3; do
  case "$state" in *'"type":"app.shutdown"'*) exit 0 ;; esac
done
`)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PROVIDER", "openai")
	t.Setenv("MODEL", "test-model")
	t.Setenv("OPENAI_API_KEY", "test-key")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runOpenTUI(ctx, factory, func(context.Context) error { return nil }, executable); err != nil {
		t.Fatalf("runOpenTUI() error = %v", err)
	}
	select {
	case <-provider.resumed:
	default:
		t.Fatal("provider did not resume after approval")
	}
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
		if _, err := parseConfigWithSettings(args, appconfig.Settings{}); err == nil {
			t.Fatalf("parseConfig(%q) error = nil", args)
		}
	}
	config, err := parseConfigWithSettings([]string{"run", "--provider", "anthropic", "--model", "test", "--workspace", root, "hello"}, appconfig.Settings{})
	if err != nil || config.workspace != root || config.prompt != "hello" {
		t.Fatalf("parseConfig() = %#v, %v", config, err)
	}
	config, err = parseConfigWithSettings([]string{"run", "--provider", "opencode", "--transport", "chat-completions", "--model", "kimi-test", "--workspace", root, "hello"}, appconfig.Settings{})
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
	if _, err := configFromTUI(tui.SetupConfig{Provider: "opencode", Model: "model", Workspace: "/workspace"}, appconfig.Settings{Transport: "invalid"}); err == nil {
		t.Fatal("configFromTUI() error = nil for invalid OpenCode transport")
	}
}

func TestConfigFromTUIConfiguresOpenCodeGoTransports(t *testing.T) {
	for _, test := range []struct {
		model     string
		transport string
	}{
		{model: "gpt-5.6-luna", transport: opencode.TransportResponses},
		{model: "minimax-m3", transport: "messages"},
		{model: "kimi-k2.7-code", transport: opencode.TransportChat},
	} {
		parsed, err := configFromTUI(tui.SetupConfig{Provider: "opencode-go", Model: test.model, Workspace: "/workspace", APIKey: "test-key"}, appconfig.Settings{})
		if err != nil || parsed.transport != test.transport || parsed.apiKey != "test-key" {
			t.Fatalf("configFromTUI(%q) = %#v, %v", test.model, parsed, err)
		}
	}
}

func TestSavedSetupRequiresCompleteProviderConnection(t *testing.T) {
	settings := appconfig.Settings{Provider: "opencode", Model: " kimi-test ", OpenCodeAPIKey: " test-key "}
	saved, ok := savedSetup(settings, "/workspace")
	if !ok || saved.Provider != "opencode" || saved.Model != "kimi-test" || saved.APIKey != "test-key" || saved.Workspace != "/workspace" {
		t.Fatalf("savedSetup() = %#v, %t", saved, ok)
	}
	for _, settings := range []appconfig.Settings{
		{Provider: "opencode", Model: "model"},
		{Provider: "opencode", OpenCodeAPIKey: "key"},
		{Model: "model", OpenCodeAPIKey: "key"},
	} {
		if _, ok := savedSetup(settings, "/workspace"); ok {
			t.Fatalf("savedSetup(%#v) = true", settings)
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

func writeUIFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "symphony-ui-fixture")
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatalf("write UI fixture: %v", err)
	}
	return path
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
