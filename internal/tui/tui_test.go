package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ch55secake/symphony/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
)

type fakeRunner struct {
	turnResult    agent.LoopResult
	resolveResult agent.LoopResult
	turnMessages  []agent.Message
	resolved      bool
	approved      bool
}

func (r *fakeRunner) Turn(_ context.Context, messages []agent.Message) (agent.LoopResult, error) {
	r.turnMessages = messages
	return r.turnResult, nil
}

func (r *fakeRunner) Resolve(_ context.Context, _ *agent.PendingApproval, approved bool) (agent.LoopResult, error) {
	r.resolved = true
	r.approved = approved
	return r.resolveResult, nil
}

func TestSubmitRetainsConversationAndRendersResponses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := &fakeRunner{turnResult: agent.LoopResult{
		Completion: &agent.Completion{Content: "I inspected the workspace."},
		Messages: []agent.Message{
			{Role: agent.RoleUser, Content: "inspect it"},
			{Role: agent.RoleAssistant, Content: "I inspected the workspace."},
		},
	}}
	m := newModel(ctx, cancel, Config{Provider: "test", Model: "model", Workspace: "/workspace", SessionID: "session"}, runner)
	m.width, m.height = 100, 30
	m.input.SetValue("inspect it")

	updated, command := m.submit()
	m = updated.(model)
	if !m.busy || len(runner.turnMessages) != 0 {
		t.Fatalf("model = %#v, runner = %#v; expected queued turn", m, runner)
	}
	updated, _ = m.Update(command())
	m = updated.(model)
	if m.busy || len(m.messages) != 2 || !strings.Contains(m.View(), "I inspected the workspace.") {
		t.Fatalf("model = %#v; expected completed rendered conversation", m)
	}
	if len(runner.turnMessages) != 1 || runner.turnMessages[0].Content != "inspect it" {
		t.Fatalf("turn messages = %#v", runner.turnMessages)
	}
}

func TestSubmitKeysSupportTerminalVariants(t *testing.T) {
	for _, key := range []string{"ctrl+s", "ctrl+j", "ctrl+enter"} {
		if !isSubmitKey(key) {
			t.Fatalf("isSubmitKey(%q) = false", key)
		}
	}
	if isSubmitKey("enter") {
		t.Fatal("plain Enter must preserve multiline input")
	}
}

func TestApprovalViewDoesNotRenderToolResultContent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := &fakeRunner{resolveResult: agent.LoopResult{Completion: &agent.Completion{Content: "write complete"}, Messages: []agent.Message{{Role: agent.RoleAssistant, Content: "write complete"}}}}
	m := newModel(ctx, cancel, Config{Provider: "test", Model: "model", Workspace: "/workspace", SessionID: "session"}, runner)
	m.width, m.height = 100, 30
	m.messages = []agent.Message{{Role: agent.RoleUser, Content: "write a file"}, {Role: agent.RoleUser, ToolResults: []agent.ToolResult{{Content: "private file content"}}}}
	m.pending = &agent.PendingApproval{Action: "write_file", Summary: "write note.txt (20 bytes)", Hash: "abc123"}
	m.refreshConversation()

	view := m.View()
	if !strings.Contains(view, "write note.txt (20 bytes)") || !strings.Contains(view, "abc123") || strings.Contains(view, "private file content") {
		t.Fatalf("view = %q", view)
	}
	updated, command := m.resolve(false)
	m = updated.(model)
	updated, _ = m.Update(command())
	m = updated.(model)
	if !runner.resolved || runner.approved || m.pending != nil || m.busy {
		t.Fatalf("model = %#v, runner = %#v; expected denied approval", m, runner)
	}
}

func TestSetupSelectsProviderAndRequiresModel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := newSetupModel(ctx, cancel, SetupConfig{Provider: "openai", Workspace: "/workspace"}, func(context.Context, string, string) ([]string, error) { return nil, nil })
	m.width = 100
	m.stage = setupConnect
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(setupModel)
	if m.provider() != "anthropic" {
		t.Fatalf("provider = %q, want anthropic", m.provider())
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(setupModel)
	if m.err != "An API key is required to connect." {
		t.Fatalf("error = %q", m.err)
	}
}

func TestSetupAcceptsConnectCommand(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := newSetupModel(ctx, cancel, SetupConfig{Workspace: "/workspace"}, func(context.Context, string, string) ([]string, error) { return nil, nil })
	if !m.command.Focused() {
		t.Fatal("command input is not focused")
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/connect")})
	m = updated.(setupModel)
	if m.command.Value() != "/connect" {
		t.Fatalf("command = %q", m.command.Value())
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(setupModel)
	if m.stage != setupConnect || !m.apiKey.Focused() {
		t.Fatalf("model = %#v; expected focused connection form", m)
	}
}

func TestSetupDisplaysDiscoveredModels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := newSetupModel(ctx, cancel, SetupConfig{Provider: "openai", Model: "gpt-test", APIKey: "test-key", Workspace: "/workspace"}, func(context.Context, string, string) ([]string, error) { return []string{"gpt-other", "gpt-test"}, nil })
	m.width, m.height = 100, 30
	m.stage = setupConnect
	updated, _ := m.Update(modelListMsg{models: []string{"gpt-other", "gpt-test"}})
	m = updated.(setupModel)
	if m.stage != setupModels || m.models[m.selected] != "gpt-test" || !strings.Contains(m.View(), "SELECT MODEL") {
		t.Fatalf("model = %#v", m)
	}
}

func TestWaitModelRendersStartupAndReturnsFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := newWaitModel(ctx, cancel, func(context.Context) error { return errors.New("Docker is unavailable") })
	m.width = 100
	if !strings.Contains(m.View(), "Starting local KurrentDB") {
		t.Fatalf("view = %q", m.View())
	}
	updated, command := m.Update(kurrentStartedMsg{err: errors.New("Docker is unavailable")})
	m = updated.(waitModel)
	if m.err == nil || command == nil {
		t.Fatalf("model = %#v; expected startup failure and quit command", m)
	}
}
