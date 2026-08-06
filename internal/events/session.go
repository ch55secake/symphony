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

// FileReadRequestedPayload identifies a requested workspace file read.
type FileReadRequestedPayload struct {
	Path string `json:"path"`
}

// FileReadCompletedPayload records metadata about a completed file read.
type FileReadCompletedPayload struct {
	Path        string `json:"path"`
	Bytes       int    `json:"bytes"`
	ContentHash string `json:"content_hash"`
	DurationMS  int64  `json:"duration_ms"`
}

// FileReadFailedPayload records safe metadata about a failed file read.
type FileReadFailedPayload struct {
	Path       string `json:"path"`
	Code       string `json:"code"`
	DurationMS int64  `json:"duration_ms"`
}
