// Package kurrentdb persists Symphony session events in KurrentDB.
package kurrentdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/ch55secake/symphony/internal/events"
	"github.com/google/uuid"
	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
)

const readBatchSize = 4096

var (
	// ErrRevisionConflict reports that the stream was not at the expected revision.
	ErrRevisionConflict = errors.New("stream revision conflict")
	// ErrSessionNotFound reports that a session stream does not exist.
	ErrSessionNotFound = errors.New("session not found")
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
		if isRevisionConflict(err) {
			return 0, fmt.Errorf("append event: %w: %w", ErrRevisionConflict, err)
		}
		return 0, fmt.Errorf("append event: %w", err)
	}
	return event.Sequence, nil
}

// Read returns all events in durable stream order and verifies their envelopes.
func (s *Store) Read(ctx context.Context, sessionID uuid.UUID) ([]events.Event, error) {
	result := make([]events.Event, 0)
	from := kurrentdb.StreamPosition(kurrentdb.Start{})
	for {
		stream, err := s.client.ReadStream(ctx, StreamName(sessionID), kurrentdb.ReadStreamOptions{
			Direction: kurrentdb.Forwards,
			From:      from,
		}, readBatchSize)
		if err != nil {
			return nil, readError(sessionID, err)
		}

		count := 0
		var lastRevision uint64
		for {
			resolved, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				stream.Close()
				return nil, readError(sessionID, err)
			}
			event, err := decode(*resolved, sessionID)
			if err != nil {
				stream.Close()
				return nil, err
			}
			result = append(result, event)
			count++
			lastRevision = resolved.OriginalEvent().EventNumber
		}
		stream.Close()
		if count < readBatchSize {
			return result, nil
		}
		from = kurrentdb.Revision(lastRevision + 1)
	}
}

// Subscribe emits future events from a session stream until ctx is cancelled.
func (s *Store) Subscribe(ctx context.Context, sessionID uuid.UUID) (<-chan events.Event, <-chan error) {
	out := make(chan events.Event)
	errs := make(chan error, 1)

	subscription, err := s.client.SubscribeToStream(ctx, StreamName(sessionID), kurrentdb.SubscribeToStreamOptions{From: kurrentdb.End{}})
	if err != nil {
		sendError(ctx, errs, fmt.Errorf("subscribe to session stream: %w", err))
		close(out)
		close(errs)
		return out, errs
	}

	go func() {
		defer close(out)
		defer close(errs)
		defer func() { _ = subscription.Close() }()

		for {
			notification := subscription.Recv()
			if notification.EventAppeared != nil {
				event, err := decode(*notification.EventAppeared, sessionID)
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
				if notification.SubscriptionDropped.Error != nil && ctx.Err() == nil && !errors.Is(notification.SubscriptionDropped.Error, context.Canceled) {
					sendError(ctx, errs, fmt.Errorf("session subscription dropped: %w", notification.SubscriptionDropped.Error))
				}
				return
			}
		}
	}()

	return out, errs
}

func decode(resolved kurrentdb.ResolvedEvent, sessionID uuid.UUID) (events.Event, error) {
	recorded := resolved.OriginalEvent()
	var event events.Event
	if err := json.Unmarshal(recorded.Data, &event); err != nil {
		return events.Event{}, fmt.Errorf("decode event %s: %w", recorded.EventID, err)
	}
	if event.SessionID != sessionID {
		return events.Event{}, fmt.Errorf("stream %q contains event for another session", StreamName(sessionID))
	}
	if event.Sequence != recorded.EventNumber {
		return events.Event{}, fmt.Errorf("event %s sequence %d does not match stream revision %d", event.ID, event.Sequence, recorded.EventNumber)
	}
	if err := event.Validate(); err != nil {
		return events.Event{}, fmt.Errorf("validate stored event %s: %w", event.ID, err)
	}
	return event, nil
}

func isRevisionConflict(err error) bool {
	var kurrentError *kurrentdb.Error
	return errors.As(err, &kurrentError) && (kurrentError.IsErrorCode(kurrentdb.ErrorCodeWrongExpectedVersion) || kurrentError.IsErrorCode(kurrentdb.ErrorCodeStreamRevisionConflict))
}

func readError(sessionID uuid.UUID, err error) error {
	var kurrentError *kurrentdb.Error
	if errors.As(err, &kurrentError) && kurrentError.IsErrorCode(kurrentdb.ErrorCodeResourceNotFound) {
		return fmt.Errorf("%w: %s: %w", ErrSessionNotFound, sessionID, err)
	}
	return fmt.Errorf("read session stream: %w", err)
}

func sendError(ctx context.Context, errs chan<- error, err error) {
	select {
	case errs <- err:
	case <-ctx.Done():
	}
}
