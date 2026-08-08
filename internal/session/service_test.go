package session

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/ch55secake/symphony/internal/audit"
	"github.com/ch55secake/symphony/internal/events"
)

type recordingStore struct {
	events   []events.Event
	expected []*uint64
}

func (s *recordingStore) Append(_ context.Context, event events.Event, expectedRevision *uint64) (uint64, error) {
	s.events = append(s.events, event)
	if expectedRevision == nil {
		s.expected = append(s.expected, nil)
		return 0, nil
	}
	revision := *expectedRevision
	s.expected = append(s.expected, &revision)
	return revision + 1, nil
}

func TestLifecycleEventsAreOrderedAndCorrelated(t *testing.T) {
	t.Parallel()
	store := &recordingStore{}
	service := New(store, audit.DefaultPolicy())
	handle, err := service.Start(context.Background(), "user", "/workspace")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := service.Finish(context.Background(), handle, "agent", "completed"); err != nil {
		t.Fatalf("Finish() error = %v", err)
	}
	if len(store.events) != 2 || store.events[0].Type != events.SessionStarted || store.events[1].Type != events.SessionFinished {
		t.Fatalf("events = %#v, want start and finish", store.events)
	}
	if store.expected[0] != nil || store.expected[1] == nil || *store.expected[1] != 0 {
		t.Fatalf("expected revisions = %#v, want nil then 0", store.expected)
	}
	if store.events[0].CorrelationID != store.events[1].CorrelationID {
		t.Fatal("lifecycle events have different correlation IDs")
	}
	if store.events[1].CausationID == nil || *store.events[1].CausationID != store.events[0].ID {
		t.Fatal("finish event does not cite the start event as its cause")
	}
	if handle.Revision != 1 {
		t.Fatalf("handle revision = %d, want 1", handle.Revision)
	}
}

func TestFailRedactsBeforePersistence(t *testing.T) {
	t.Parallel()
	store := &recordingStore{}
	service := New(store, audit.DefaultPolicy())
	handle, err := service.Start(context.Background(), "user", "/workspace")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := service.Fail(context.Background(), handle, "agent", "Bearer top-secret"); err != nil {
		t.Fatalf("Fail() error = %v", err)
	}
	var payload events.SessionFailedPayload
	if err := json.Unmarshal(store.events[1].Payload, &payload); err != nil {
		t.Fatalf("unmarshal failure payload: %v", err)
	}
	if payload.Message != audit.RedactedValue || len(store.events[1].Redactions) != 1 {
		t.Fatalf("failure event = %#v, want redacted message", store.events[1])
	}
}

func TestTerminalEventsCanOnlyBeAppendedOnce(t *testing.T) {
	t.Parallel()
	store := &recordingStore{}
	service := New(store, audit.DefaultPolicy())
	handle, err := service.Start(context.Background(), "user", "/workspace")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := service.Finish(context.Background(), handle, "agent", "completed"); err != nil {
		t.Fatalf("Finish() error = %v", err)
	}
	if err := service.Fail(context.Background(), handle, "agent", "late failure"); !errors.Is(err, ErrClosed) {
		t.Fatalf("Fail() error = %v, want ErrClosed", err)
	}
	if len(store.events) != 2 {
		t.Fatalf("events = %d, want 2", len(store.events))
	}
}

func TestConcurrentTerminalEventsSerializeHandleState(t *testing.T) {
	t.Parallel()
	store := &recordingStore{}
	service := New(store, audit.DefaultPolicy())
	handle, err := service.Start(context.Background(), "user", "/workspace")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var group sync.WaitGroup
	for _, closeSession := range []func() error{
		func() error {
			<-start
			return service.Finish(context.Background(), handle, "agent", "completed")
		},
		func() error {
			<-start
			return service.Fail(context.Background(), handle, "agent", "failed")
		},
	} {
		group.Go(func() {
			errs <- closeSession()
		})
	}
	close(start)
	group.Wait()
	close(errs)

	succeeded := 0
	for err := range errs {
		if err == nil {
			succeeded++
			continue
		}
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("terminal event error = %v, want ErrClosed", err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("successful terminal events = %d, want 1", succeeded)
	}
	if len(store.events) != 2 {
		t.Fatalf("events = %d, want start plus one terminal event", len(store.events))
	}
}
