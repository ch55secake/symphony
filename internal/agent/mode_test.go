package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ch55secake/symphony/internal/audit"
	"github.com/ch55secake/symphony/internal/events"
	"github.com/ch55secake/symphony/internal/session"
)

func TestToolsForMode(t *testing.T) {
	tools := []ToolDefinition{{Name: "read_file"}, {Name: "write_file"}, {Name: "run_command"}}
	plan := ToolsForMode(tools, ModePlan)
	if len(plan) != 2 || plan[0].Name != "read_file" || plan[1].Name != "run_command" {
		t.Fatalf("plan tools = %#v, want read_file and run_command", plan)
	}
	build := ToolsForMode(tools, ModeBuild)
	if len(build) != len(tools) {
		t.Fatalf("build tools = %#v, want all tools", build)
	}
}

func TestModeInstructions(t *testing.T) {
	if got := ModePlan.Instructions(); got == "" || !containsAll(got, "Plan mode", "Do not modify files") {
		t.Fatalf("plan instructions = %q", got)
	}
	if got := ModeBuild.Instructions(); got == "" || !containsAll(got, "Build mode", "Implement") {
		t.Fatalf("build instructions = %q", got)
	}
}

func TestLoopRejectsToolUnavailableInActiveMode(t *testing.T) {
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
	provider := &sequentialProvider{completions: []Completion{{ToolCalls: []events.ModelToolCall{{ID: "call-1", Name: "write_file", Arguments: json.RawMessage(`{}`)}}}}}
	_, err = loop.RunWithApproval(context.Background(), handle, "user", provider, CompletionRequest{Model: "test-model", Instructions: ModePlan.Instructions(), Messages: []Message{{Role: RoleUser, Content: "plan"}}, Tools: ToolsForMode([]ToolDefinition{{Name: "read_file"}, {Name: "write_file"}}, ModePlan)})
	if !errors.Is(err, ErrToolUnavailable) {
		t.Fatalf("RunWithApproval() error = %v, want ErrToolUnavailable", err)
	}
}

func containsAll(value string, terms ...string) bool {
	for _, term := range terms {
		if !strings.Contains(value, term) {
			return false
		}
	}
	return true
}
