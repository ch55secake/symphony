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

// SessionConfigChangedPayload records a safe runtime setting change.
type SessionConfigChangedPayload struct {
	Setting  string `json:"setting"`
	Previous string `json:"previous"`
	Current  string `json:"current"`
}

// ApprovalModeChangedPayload records whether approval prompts are enabled.
type ApprovalModeChangedPayload struct {
	AllowAll bool `json:"allow_all"`
}

// ModelListRequestedPayload identifies a provider catalog request without credentials.
type ModelListRequestedPayload struct {
	Provider string `json:"provider"`
}

// ModelListCompletedPayload records the number of models returned by a catalog request.
type ModelListCompletedPayload struct {
	Provider string `json:"provider"`
	Count    int    `json:"count"`
}

// ModelListFailedPayload records a safe catalog request failure code.
type ModelListFailedPayload struct {
	Provider string `json:"provider"`
	Code     string `json:"code"`
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

// FileWriteRequestedPayload records the content identity proposed for a file write.
type FileWriteRequestedPayload struct {
	OperationID string `json:"operation_id"`
	Path        string `json:"path"`
	Bytes       int    `json:"bytes"`
	ContentHash string `json:"content_hash"`
}

// FileWriteApprovedPayload records an operator's approval of a proposed file write.
type FileWriteApprovedPayload struct {
	OperationID string `json:"operation_id"`
}

// FileWriteCompletedPayload records metadata about a completed file write.
type FileWriteCompletedPayload struct {
	OperationID string `json:"operation_id"`
	Path        string `json:"path"`
	Bytes       int    `json:"bytes"`
	ContentHash string `json:"content_hash"`
	DurationMS  int64  `json:"duration_ms"`
}

// FileWriteFailedPayload records safe metadata about a failed file write.
type FileWriteFailedPayload struct {
	OperationID string `json:"operation_id"`
	Path        string `json:"path"`
	Code        string `json:"code"`
	DurationMS  int64  `json:"duration_ms"`
}
