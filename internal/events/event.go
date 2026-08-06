// Package events defines the immutable records that make up a Symphony session.
package events

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const SchemaVersion = 1

type Type string

const (
	SessionStarted     Type = "session.started"
	SessionResumed     Type = "session.resumed"
	SessionFinished    Type = "session.finished"
	SessionFailed      Type = "session.failed"
	FileReadRequested  Type = "file.read.requested"
	FileReadCompleted  Type = "file.read.completed"
	FileReadFailed     Type = "file.read.failed"
	FileWriteRequested Type = "file.write.requested"
	FileWriteApproved  Type = "file.write.approved"
	FileWriteCompleted Type = "file.write.completed"
	FileWriteFailed    Type = "file.write.failed"
	CommandRequested   Type = "command.requested"
	CommandApproved    Type = "command.approved"
	CommandCompleted   Type = "command.completed"
	CommandFailed      Type = "command.failed"
	UserMessage        Type = "user.message"
	ModelRequested     Type = "model.requested"
	ModelCompleted     Type = "model.completed"
	ModelFailed        Type = "model.failed"
	PolicyRedacted     Type = "policy.redacted"
)

// Redaction identifies a field intentionally omitted from a durable event.
type Redaction struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Event is the versioned, immutable record persisted to a KurrentDB stream.
type Event struct {
	ID            uuid.UUID       `json:"id"`
	SessionID     uuid.UUID       `json:"session_id"`
	Sequence      uint64          `json:"sequence"`
	Type          Type            `json:"type"`
	SchemaVersion int             `json:"schema_version"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Actor         string          `json:"actor"`
	CorrelationID uuid.UUID       `json:"correlation_id"`
	CausationID   *uuid.UUID      `json:"causation_id,omitempty"`
	Payload       json.RawMessage `json:"payload"`
	PayloadHash   string          `json:"payload_hash"`
	Redactions    []Redaction     `json:"redactions,omitempty"`
}

// New builds an event with a canonical payload hash. Sequence is assigned by the store.
func New(sessionID uuid.UUID, eventType Type, actor string, correlationID uuid.UUID, payload any, redactions []Redaction) (Event, error) {
	if sessionID == uuid.Nil {
		return Event{}, fmt.Errorf("session ID is required")
	}
	if eventType == "" {
		return Event{}, fmt.Errorf("event type is required")
	}
	if actor == "" {
		return Event{}, fmt.Errorf("actor is required")
	}
	if correlationID == uuid.Nil {
		return Event{}, fmt.Errorf("correlation ID is required")
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return Event{}, fmt.Errorf("marshal event payload: %w", err)
	}

	return Event{
		ID:            uuid.New(),
		SessionID:     sessionID,
		Type:          eventType,
		SchemaVersion: SchemaVersion,
		OccurredAt:    time.Now().UTC(),
		Actor:         actor,
		CorrelationID: correlationID,
		Payload:       encoded,
		PayloadHash:   Hash(encoded),
		Redactions:    redactions,
	}, nil
}

// Hash returns the lowercase SHA-256 hash of data.
func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (e Event) Validate() error {
	if e.ID == uuid.Nil || e.SessionID == uuid.Nil || e.CorrelationID == uuid.Nil {
		return fmt.Errorf("event, session, and correlation IDs are required")
	}
	if e.Type == "" || e.Actor == "" || e.SchemaVersion < 1 {
		return fmt.Errorf("event type, actor, and schema version are required")
	}
	if !json.Valid(e.Payload) {
		return fmt.Errorf("event payload is not valid JSON")
	}
	if e.PayloadHash != Hash(e.Payload) {
		return fmt.Errorf("event payload hash does not match payload")
	}
	return nil
}
