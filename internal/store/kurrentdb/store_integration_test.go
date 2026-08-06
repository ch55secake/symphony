package kurrentdb

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/ch55secake/symphony/internal/events"
	"github.com/google/uuid"
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
	second, err := events.New(sessionID, events.SessionResumed, "user", uuid.New(), map[string]string{"workspace": "/tmp/project"}, nil)
	if err != nil {
		t.Fatalf("New second event: %v", err)
	}
	revision, err = store.Append(ctx, second, &revision)
	if err != nil {
		t.Fatalf("Append second event: %v", err)
	}
	if revision != 1 {
		t.Fatalf("revision = %d, want 1", revision)
	}

	stale, err := events.New(sessionID, events.SessionFinished, "user", uuid.New(), nil, nil)
	if err != nil {
		t.Fatalf("New stale event: %v", err)
	}
	staleRevision := uint64(0)
	if _, err := store.Append(ctx, stale, &staleRevision); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("Append stale event error = %v, want ErrRevisionConflict", err)
	}

	stored, err := store.Read(ctx, sessionID)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(stored) != 2 || stored[0].ID != event.ID || stored[0].Sequence != 0 || stored[1].ID != second.ID || stored[1].Sequence != 1 {
		t.Fatalf("stored events = %#v, want two persisted events", stored)
	}

	subscriptionContext, stopSubscription := context.WithCancel(ctx)
	live, subscriptionErrors := store.Subscribe(subscriptionContext, sessionID)
	liveEvent, err := events.New(sessionID, events.SessionFinished, "user", uuid.New(), nil, nil)
	if err != nil {
		t.Fatalf("New live event: %v", err)
	}
	if _, err := store.Append(ctx, liveEvent, &revision); err != nil {
		t.Fatalf("Append live event: %v", err)
	}
	select {
	case received := <-live:
		if received.ID != liveEvent.ID {
			t.Fatalf("received event ID = %s, want %s", received.ID, liveEvent.ID)
		}
	case err := <-subscriptionErrors:
		t.Fatalf("Subscribe() error = %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("Subscribe() did not receive the live event")
	}
	stopSubscription()
	select {
	case _, ok := <-live:
		if ok {
			t.Fatal("subscription event channel remained open after cancellation")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("subscription did not stop after cancellation")
	}
	if _, ok := <-subscriptionErrors; ok {
		t.Fatal("subscription error channel emitted an error after cancellation")
	}
}
