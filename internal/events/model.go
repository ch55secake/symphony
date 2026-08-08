package events

import "encoding/json"

// UserMessagePayload records user-provided turn content.
type UserMessagePayload struct {
	Content string `json:"content"`
}

// ModelRequestedPayload identifies an outbound model completion request.
type ModelRequestedPayload struct {
	Provider     string   `json:"provider"`
	Model        string   `json:"model"`
	RequestHash  string   `json:"request_hash"`
	MessageCount int      `json:"message_count"`
	ToolNames    []string `json:"tool_names,omitempty"`
}

// ModelToolCall records a provider-requested tool invocation without executing it.
type ModelToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ModelCompletedPayload records a completed model response.
type ModelCompletedPayload struct {
	Provider     string          `json:"provider"`
	Model        string          `json:"model"`
	Content      string          `json:"content"`
	ToolCalls    []ModelToolCall `json:"tool_calls,omitempty"`
	StopReason   string          `json:"stop_reason"`
	InputTokens  int             `json:"input_tokens,omitempty"`
	OutputTokens int             `json:"output_tokens,omitempty"`
}

// ModelFailedPayload records safe metadata about a failed model request.
type ModelFailedPayload struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Code     string `json:"code"`
}

// ToolResultPayload records a model tool result before it is sent to a provider.
type ToolResultPayload struct {
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	IsError   bool   `json:"is_error"`
	Bytes     int    `json:"bytes"`
	Hash      string `json:"hash"`
	Truncated bool   `json:"truncated"`
}
