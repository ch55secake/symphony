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

func TestReadPersistsAuditedEvents(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("audited content"), 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	sessions := session.New(store, audit.DefaultPolicy())
	handle, err := sessions.Start(ctx, "user", root)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	reads, err := workspace.New(sessions, root)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := reads.Read(ctx, handle, "agent", "note.txt"); err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	persisted, err := store.Read(ctx, handle.SessionID)
	if err != nil {
		t.Fatalf("Read persisted events: %v", err)
	}
	if len(persisted) != 3 || persisted[1].Type != events.FileReadRequested || persisted[2].Type != events.FileReadCompleted {
		t.Fatalf("persisted events = %#v, want audited file read", persisted)
	}
}
