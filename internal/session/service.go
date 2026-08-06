// Package session owns ordered lifecycle events for an agent session.
package session

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ch55secake/symphony/internal/audit"
	"github.com/ch55secake/symphony/internal/events"
	"github.com/google/uuid"
)

// ErrClosed reports an attempt to append a terminal event twice.
var ErrClosed = errors.New("session is already closed")

// EventStore is the ordered append capability required by the session runtime.
type EventStore interface {
	Append(context.Context, events.Event, *uint64) (uint64, error)
}

// Service persists session lifecycle events after applying its audit policy.
type Service struct {
	store  EventStore
	policy audit.Policy
}

// Handle carries the ordered write state for one session.
type Handle struct {
	mu            sync.Mutex
	SessionID     uuid.UUID
	CorrelationID uuid.UUID
	Revision      uint64
	closed        bool
	lastEventID   uuid.UUID
}

func New(store EventStore, policy audit.Policy) *Service {
	return &Service{store: store, policy: policy}
}

// Start creates a new session stream and persists its first lifecycle event.
func (s *Service) Start(ctx context.Context, actor, workspace string) (*Handle, error) {
	handle := &Handle{SessionID: uuid.New(), CorrelationID: uuid.New()}
	event, err := s.event(handle, events.SessionStarted, actor, events.SessionStartedPayload{Workspace: workspace})
	if err != nil {
		return nil, err
	}
	revision, err := s.store.Append(ctx, event, nil)
	if err != nil {
		return nil, fmt.Errorf("append session start: %w", err)
	}
	handle.Revision = revision
	handle.lastEventID = event.ID
	return handle, nil
}

// Finish records a successful terminal event for handle.
func (s *Service) Finish(ctx context.Context, handle *Handle, actor, reason string) error {
	return s.close(ctx, handle, events.SessionFinished, actor, events.SessionFinishedPayload{Reason: reason})
}

// Fail records a failed terminal event for handle.
func (s *Service) Fail(ctx context.Context, handle *Handle, actor, message string) error {
	return s.close(ctx, handle, events.SessionFailed, actor, events.SessionFailedPayload{Message: message})
}

func (s *Service) close(ctx context.Context, handle *Handle, eventType events.Type, actor string, payload any) error {
	if handle == nil {
		return fmt.Errorf("session handle is required")
	}
	handle.mu.Lock()
	defer handle.mu.Unlock()
	if handle.closed {
		return ErrClosed
	}
	event, err := s.event(handle, eventType, actor, payload)
	if err != nil {
		return err
	}
	revision, err := s.store.Append(ctx, event, &handle.Revision)
	if err != nil {
		return fmt.Errorf("append session terminal event: %w", err)
	}
	handle.Revision = revision
	handle.lastEventID = event.ID
	handle.closed = true
	return nil
}

func (s *Service) event(handle *Handle, eventType events.Type, actor string, payload any) (events.Event, error) {
	redacted, redactions, err := s.policy.Redact(payload)
	if err != nil {
		return events.Event{}, fmt.Errorf("redact session payload: %w", err)
	}
	event, err := events.New(handle.SessionID, eventType, actor, handle.CorrelationID, redacted, redactions)
	if err != nil {
		return events.Event{}, fmt.Errorf("create session event: %w", err)
	}
	if handle.lastEventID != uuid.Nil {
		causationID := handle.lastEventID
		event.CausationID = &causationID
	}
	return event, nil
}
