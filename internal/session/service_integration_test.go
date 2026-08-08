package session_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ch55secake/symphony/internal/audit"
	"github.com/ch55secake/symphony/internal/events"
	"github.com/ch55secake/symphony/internal/session"
	"github.com/ch55secake/symphony/internal/store/kurrentdb"
)

func TestLifecyclePersistsOrderedEvents(t *testing.T) {
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
	service := session.New(store, audit.DefaultPolicy())
	handle, err := service.Start(ctx, "user", "/workspace")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := service.Finish(ctx, handle, "agent", "completed"); err != nil {
		t.Fatalf("Finish() error = %v", err)
	}

	persisted, err := store.Read(ctx, handle.SessionID)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(persisted) != 2 || persisted[0].Type != events.SessionStarted || persisted[1].Type != events.SessionFinished {
		t.Fatalf("persisted events = %#v, want ordered lifecycle events", persisted)
	}
	if persisted[1].CausationID == nil || *persisted[1].CausationID != persisted[0].ID {
		t.Fatal("finished event does not reference started event")
	}
}
