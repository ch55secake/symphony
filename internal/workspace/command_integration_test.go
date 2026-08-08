package workspace_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ch55secake/symphony/internal/audit"
	"github.com/ch55secake/symphony/internal/events"
	"github.com/ch55secake/symphony/internal/session"
	"github.com/ch55secake/symphony/internal/store/kurrentdb"
	"github.com/ch55secake/symphony/internal/workspace"
)

func TestCommandPersistsApprovedAuditEvents(t *testing.T) {
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
	root := t.TempDir()
	sessions := session.New(store, audit.DefaultPolicy())
	handle, err := sessions.Start(ctx, "user", root)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	commands, err := workspace.New(sessions, root)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	command := workspace.Command{Executable: "sh", Arguments: []string{"-c", "printf audited"}}
	request, err := commands.RequestCommand(ctx, handle, "agent", command)
	if err != nil {
		t.Fatalf("RequestCommand() error = %v", err)
	}
	if err := commands.ApproveCommand(ctx, handle, "user", request); err != nil {
		t.Fatalf("ApproveCommand() error = %v", err)
	}
	if _, err := commands.ExecuteCommand(ctx, handle, "agent", request, command); err != nil {
		t.Fatalf("ExecuteCommand() error = %v", err)
	}

	persisted, err := store.Read(ctx, handle.SessionID)
	if err != nil {
		t.Fatalf("Read persisted events: %v", err)
	}
	if len(persisted) != 4 || persisted[1].Type != events.CommandRequested || persisted[2].Type != events.CommandApproved || persisted[3].Type != events.CommandCompleted {
		t.Fatalf("persisted events = %#v, want audited command lifecycle", persisted)
	}
}
