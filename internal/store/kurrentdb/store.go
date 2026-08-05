// Package kurrentdb persists Symphony session events in KurrentDB.
package kurrentdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	"github.com/ch55secake/symphony/internal/events"
)

// Store appends and reads one immutable KurrentDB stream per agent session.
type Store struct {
	client *kurrentdb.Client
}

func New(connectionString string) (*Store, error) {
	configuration, err := kurrentdb.ParseConnectionString(connectionString)
	if err != nil {
		return nil, fmt.Errorf("parse KurrentDB connection string: %w", err)
	}
	client, err := kurrentdb.NewClient(configuration)
	if err != nil {
		return nil, fmt.Errorf("create KurrentDB client: %w", err)
	}
	return &Store{client: client}, nil
}

func (s *Store) Close() error {
	return s.client.Close()
}

func StreamName(sessionID uuid.UUID) string {
	return "session-" + sessionID.String()
}

// Append persists event only if the stream is at expectedRevision. A nil revision
// requires a new stream. The returned revision is zero-based.
func (s *Store) Append(ctx context.Context, event events.Event, expectedRevision *uint64) (uint64, error) {
	if event.Sequence != 0 {
		return 0, fmt.Errorf("event sequence is assigned by the store")
	}
	if expectedRevision != nil {
		event.Sequence = *expectedRevision + 1
	}
	if err := event.Validate(); err != nil {
		return 0, fmt.Errorf("validate event: %w", err)
	}

	encoded, err := json.Marshal(event)
	if err != nil {
		return 0, fmt.Errorf("marshal event envelope: %w", err)
	}

	expected := kurrentdb.StreamState(kurrentdb.NoStream{})
	if expectedRevision != nil {
		expected = kurrentdb.Revision(*expectedRevision)
	}
	_, err = s.client.AppendToStream(ctx, StreamName(event.SessionID), kurrentdb.AppendToStreamOptions{
		StreamState: expected,
	}, kurrentdb.EventData{
		EventID:     event.ID,
		EventType:   string(event.Type),
		ContentType: kurrentdb.ContentTypeJson,
		Data:        encoded,
	})
	if err != nil {
		return 0, fmt.Errorf("append event: %w", err)
	}
	return event.Sequence, nil
}

// Read returns all events in durable stream order and verifies their envelopes.
func (s *Store) Read(ctx context.Context, sessionID uuid.UUID) ([]events.Event, error) {
	stream, err := s.client.ReadStream(ctx, StreamName(sessionID), kurrentdb.ReadStreamOptions{
		Direction: kurrentdb.Forwards,
		From:      kurrentdb.Start{},
	}, 4096)
	if err != nil {
		return nil, fmt.Errorf("read session stream: %w", err)
	}
	defer stream.Close()

	result := make([]events.Event, 0)
	for {
		resolved, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return result, nil
		}
		if err != nil {
			return nil, fmt.Errorf("receive session event: %w", err)
		}
		event, err := decode(*resolved)
		if err != nil {
			return nil, err
		}
		if event.SessionID != sessionID {
			return nil, fmt.Errorf("stream %q contains event for another session", StreamName(sessionID))
		}
		result = append(result, event)
	}
}

// Subscribe emits future events from a session stream until ctx is cancelled.
func (s *Store) Subscribe(ctx context.Context, sessionID uuid.UUID) (<-chan events.Event, <-chan error) {
	out := make(chan events.Event)
	errs := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errs)

		subscription, err := s.client.SubscribeToStream(ctx, StreamName(sessionID), kurrentdb.SubscribeToStreamOptions{From: kurrentdb.End{}})
		if err != nil {
			sendError(ctx, errs, fmt.Errorf("subscribe to session stream: %w", err))
			return
		}
		defer subscription.Close()

		for {
			notification := subscription.Recv()
			if notification.EventAppeared != nil {
				event, err := decode(*notification.EventAppeared)
				if err != nil {
					sendError(ctx, errs, err)
					return
				}
				select {
				case out <- event:
				case <-ctx.Done():
					return
				}
			}
			if notification.SubscriptionDropped != nil {
				if notification.SubscriptionDropped.Error != nil && !errors.Is(notification.SubscriptionDropped.Error, context.Canceled) {
					sendError(ctx, errs, fmt.Errorf("session subscription dropped: %w", notification.SubscriptionDropped.Error))
				}
				return
			}
		}
	}()

	return out, errs
}

func decode(resolved kurrentdb.ResolvedEvent) (events.Event, error) {
	recorded := resolved.OriginalEvent()
	var event events.Event
	if err := json.Unmarshal(recorded.Data, &event); err != nil {
		return events.Event{}, fmt.Errorf("decode event %s: %w", recorded.EventID, err)
	}
	if event.Sequence != recorded.EventNumber {
		return events.Event{}, fmt.Errorf("event %s sequence %d does not match stream revision %d", event.ID, event.Sequence, recorded.EventNumber)
	}
	if err := event.Validate(); err != nil {
		return events.Event{}, fmt.Errorf("validate stored event %s: %w", event.ID, err)
	}
	return event, nil
}

func sendError(ctx context.Context, errs chan<- error, err error) {
	select {
	case errs <- err:
	case <-ctx.Done():
	}
}
