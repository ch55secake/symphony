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
