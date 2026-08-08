package events

// CommandRequestedPayload identifies a command proposed for workspace execution.
type CommandRequestedPayload struct {
	OperationID      string   `json:"operation_id"`
	Executable       string   `json:"executable"`
	Arguments        []string `json:"arguments"`
	WorkingDirectory string   `json:"working_directory"`
	CommandHash      string   `json:"command_hash"`
}

// CommandApprovedPayload records an operator's approval of a proposed command.
type CommandApprovedPayload struct {
	OperationID string `json:"operation_id"`
	CommandHash string `json:"command_hash"`
}

// CommandOutputMetadata identifies bounded command output without persisting it.
type CommandOutputMetadata struct {
	Bytes         int64  `json:"bytes"`
	CapturedBytes int    `json:"captured_bytes"`
	Hash          string `json:"hash"`
	Truncated     bool   `json:"truncated"`
}

// CommandCompletedPayload records a successful command outcome.
type CommandCompletedPayload struct {
	OperationID string                `json:"operation_id"`
	CommandHash string                `json:"command_hash"`
	ExitCode    int                   `json:"exit_code"`
	Stdout      CommandOutputMetadata `json:"stdout"`
	Stderr      CommandOutputMetadata `json:"stderr"`
	DurationMS  int64                 `json:"duration_ms"`
}

// CommandFailedPayload records safe metadata about a failed command outcome.
type CommandFailedPayload struct {
	OperationID string                `json:"operation_id"`
	CommandHash string                `json:"command_hash"`
	Code        string                `json:"code"`
	ExitCode    int                   `json:"exit_code"`
	Stdout      CommandOutputMetadata `json:"stdout"`
	Stderr      CommandOutputMetadata `json:"stderr"`
	DurationMS  int64                 `json:"duration_ms"`
}
