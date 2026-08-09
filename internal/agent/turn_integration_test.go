package agent_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ch55secake/symphony/internal/agent"
	"github.com/ch55secake/symphony/internal/audit"
	"github.com/ch55secake/symphony/internal/events"
	"github.com/ch55secake/symphony/internal/session"
	"github.com/ch55secake/symphony/internal/store/kurrentdb"
)

type fakeProvider struct{}

func (fakeProvider) Name() string {
	return "fake"
}

func (fakeProvider) Complete(_ context.Context, _ agent.CompletionRequest) (agent.Completion, error) {
	return agent.Completion{Content: "response", StopReason: "stop", InputTokens: 1, OutputTokens: 1}, nil
}

func TestTurnPersistsModelLifecycle(t *testing.T) {
	connectionString := os.Getenv("KURRENTDB_URL")
	if connectionString == "" {
		t.Skip("KURRENTDB_URL is not set")
	}
	store, err := kurrentdb.New(connectionString)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	sessions := session.New(store, audit.DefaultPolicy())
	handle, err := sessions.Start(ctx, "user", "/workspace")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	turns, err := agent.New(sessions)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := turns.Run(ctx, handle, "user", fakeProvider{}, agent.CompletionRequest{
		Model:    "test-model",
		Messages: []agent.Message{{Role: agent.RoleUser, Content: "hello"}},
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	persisted, err := store.Read(ctx, handle.SessionID)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(persisted) != 4 || persisted[1].Type != events.UserMessage || persisted[2].Type != events.ModelRequested || persisted[3].Type != events.ModelCompletedV2 {
		t.Fatalf("persisted events = %#v, want model lifecycle", persisted)
	}
}
