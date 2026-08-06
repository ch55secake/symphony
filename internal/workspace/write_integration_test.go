package workspace_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ch55secake/symphony/internal/audit"
	"github.com/ch55secake/symphony/internal/events"
	"github.com/ch55secake/symphony/internal/session"
	"github.com/ch55secake/symphony/internal/store/kurrentdb"
	"github.com/ch55secake/symphony/internal/workspace"
)

func TestWritePersistsApprovedAuditEvents(t *testing.T) {
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
	writes, err := workspace.New(sessions, root)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	content := []byte("audited write")
	request, err := writes.RequestWrite(ctx, handle, "agent", "note.txt", content)
	if err != nil {
		t.Fatalf("RequestWrite() error = %v", err)
	}
	if err := writes.ApproveWrite(ctx, handle, "user", request); err != nil {
		t.Fatalf("ApproveWrite() error = %v", err)
	}
	if err := writes.ExecuteWrite(ctx, handle, "agent", request, content); err != nil {
		t.Fatalf("ExecuteWrite() error = %v", err)
	}
	if persisted, err := os.ReadFile(filepath.Join(root, "note.txt")); err != nil || string(persisted) != string(content) {
		t.Fatalf("written file = %q, %v", persisted, err)
	}

	persisted, err := store.Read(ctx, handle.SessionID)
	if err != nil {
		t.Fatalf("Read persisted events: %v", err)
	}
	if len(persisted) != 4 || persisted[1].Type != events.FileWriteRequested || persisted[2].Type != events.FileWriteApproved || persisted[3].Type != events.FileWriteCompleted {
		t.Fatalf("persisted events = %#v, want audited write lifecycle", persisted)
	}
}
