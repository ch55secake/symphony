package events

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestNewCreatesValidEvent(t *testing.T) {
	t.Parallel()
	payload := map[string]string{"message": "hello"}
	event, err := New(uuid.New(), SessionStarted, "user", uuid.New(), payload, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := event.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if event.PayloadHash != Hash(event.Payload) {
		t.Fatal("payload hash was not calculated from payload")
	}
}

func TestValidateRejectsChangedPayload(t *testing.T) {
	t.Parallel()
	event, err := New(uuid.New(), SessionStarted, "user", uuid.New(), map[string]string{"message": "hello"}, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	event.Payload = json.RawMessage(`{"message":"changed"}`)
	if err := event.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want hash mismatch")
	}
}

func TestNewRejectsMissingFields(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		sessionID     uuid.UUID
		eventType     Type
		actor         string
		correlationID uuid.UUID
	}{
		"missing session ID":     {uuid.Nil, SessionStarted, "user", uuid.New()},
		"missing event type":     {uuid.New(), "", "user", uuid.New()},
		"missing actor":          {uuid.New(), SessionStarted, "", uuid.New()},
		"missing correlation ID": {uuid.New(), SessionStarted, "user", uuid.Nil},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := New(test.sessionID, test.eventType, test.actor, test.correlationID, nil, nil); err == nil {
				t.Fatal("New() error = nil, want validation error")
			}
		})
	}
}

func TestValidateRejectsInvalidEnvelope(t *testing.T) {
	t.Parallel()
	tests := map[string]func(*Event){
		"missing IDs": func(event *Event) {
			event.ID = uuid.Nil
		},
		"missing event type": func(event *Event) {
			event.Type = ""
		},
		"missing actor": func(event *Event) {
			event.Actor = ""
		},
		"invalid schema version": func(event *Event) {
			event.SchemaVersion = 0
		},
		"invalid JSON payload": func(event *Event) {
			event.Payload = json.RawMessage(`{`)
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			event, err := New(uuid.New(), SessionStarted, "user", uuid.New(), map[string]string{"message": "hello"}, nil)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			mutate(&event)
			if err := event.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want validation error")
			}
		})
	}
}
