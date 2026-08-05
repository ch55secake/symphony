package kurrentdb

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ch55secake/symphony/internal/events"
)

func TestStoreAppendAndRead(t *testing.T) {
	connectionString := os.Getenv("KURRENTDB_URL")
	if connectionString == "" {
		t.Skip("KURRENTDB_URL is not set")
	}
	store, err := New(connectionString)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	sessionID := uuid.New()
	event, err := events.New(sessionID, events.SessionStarted, "user", uuid.New(), map[string]string{"workspace": "/tmp/project"}, nil)
	if err != nil {
		t.Fatalf("New event: %v", err)
	}
	revision, err := store.Append(ctx, event, nil)
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if revision != 0 {
		t.Fatalf("revision = %d, want 0", revision)
	}

	stored, err := store.Read(ctx, sessionID)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(stored) != 1 || stored[0].ID != event.ID || stored[0].Sequence != 0 {
		t.Fatalf("stored events = %#v, want one persisted event", stored)
	}
}
