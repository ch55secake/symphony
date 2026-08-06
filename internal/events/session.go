package events

// SessionStartedPayload identifies the workspace a session is operating in.
type SessionStartedPayload struct {
	Workspace string `json:"workspace"`
}

// SessionFinishedPayload records why an agent session completed.
type SessionFinishedPayload struct {
	Reason string `json:"reason,omitempty"`
}

// SessionFailedPayload records a safe failure message for an agent session.
type SessionFailedPayload struct {
	Message string `json:"message"`
}
