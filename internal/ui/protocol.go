// Package ui defines the private protocol between Symphony's Go runtime and UI child.
package ui

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const Version = 1
const maxMessageBytes = 1 << 20

// Message is a versioned JSON-lines RPC envelope.
type Message struct {
	Version int             `json:"version"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// State is the display-only session snapshot sent from Go to OpenTUI.
type State struct {
	Phase      string            `json:"phase"`
	Provider   string            `json:"provider,omitempty"`
	Model      string            `json:"model,omitempty"`
	Theme      string            `json:"theme,omitempty"`
	Workspace  string            `json:"workspace,omitempty"`
	Status     string            `json:"status,omitempty"`
	Transcript []TranscriptEntry `json:"transcript,omitempty"`
	Approval   *Approval         `json:"approval,omitempty"`
	Selection  string            `json:"selection,omitempty"`
	Options    []string          `json:"options,omitempty"`
	AllowAll   bool              `json:"allow_all,omitempty"`
}

// Approval contains only the operator-safe details needed by the UI.
type Approval struct {
	Action  string `json:"action"`
	Summary string `json:"summary"`
	Hash    string `json:"hash"`
}

// TranscriptEntry is safe display metadata for a conversation message.
type TranscriptEntry struct {
	Role    string        `json:"role"`
	Label   string        `json:"label"`
	Content string        `json:"content,omitempty"`
	Tool    *ToolActivity `json:"tool,omitempty"`
}

// ToolActivity is a display-safe tool lifecycle update.
type ToolActivity struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Phase            string `json:"phase"`
	Target           string `json:"target,omitempty"`
	Command          string `json:"command,omitempty"`
	WorkingDirectory string `json:"working_directory,omitempty"`
	Bytes            int    `json:"bytes,omitempty"`
	Truncated        bool   `json:"truncated,omitempty"`
	ExitCode         *int   `json:"exit_code,omitempty"`
	OutputHidden     bool   `json:"output_hidden,omitempty"`
}

// SendState writes a display state without exposing backend capabilities.
func SendState(writer io.Writer, state State) error {
	for {
		payload, err := json.Marshal(state)
		if err != nil {
			return fmt.Errorf("encode UI state: %w", err)
		}
		if len(state.Transcript) == 0 || len(payload)+128 < maxMessageBytes {
			return Write(writer, Message{Type: "state", Payload: payload})
		}
		state.Transcript = state.Transcript[1:]
	}
}

// Read decodes one bounded protocol message.
func Read(reader *bufio.Reader) (Message, error) {
	line := make([]byte, 0, 4096)
	for {
		fragment, err := reader.ReadSlice('\n')
		if len(line)+len(fragment) > maxMessageBytes {
			return Message{}, fmt.Errorf("UI protocol message exceeds %d bytes", maxMessageBytes)
		}
		line = append(line, fragment...)
		if err == nil {
			break
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return Message{}, err
		}
	}
	var message Message
	if err := json.Unmarshal(line, &message); err != nil {
		return Message{}, fmt.Errorf("decode UI protocol message: %w", err)
	}
	if message.Version != Version {
		return Message{}, fmt.Errorf("unsupported UI protocol version %d", message.Version)
	}
	if message.Type == "" {
		return Message{}, fmt.Errorf("UI protocol message type is required")
	}
	return message, nil
}

// Write encodes one protocol message without using the terminal streams.
func Write(writer io.Writer, message Message) error {
	message.Version = Version
	encoded, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode UI protocol message: %w", err)
	}
	if len(encoded)+1 > maxMessageBytes {
		return fmt.Errorf("UI protocol message exceeds %d bytes", maxMessageBytes)
	}
	if _, err := writer.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("write UI protocol message: %w", err)
	}
	return nil
}
