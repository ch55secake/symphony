// Package ui defines the private protocol between Symphony's Go runtime and UI child.
package ui

import (
	"bufio"
	"encoding/json"
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
	Phase      string   `json:"phase"`
	Provider   string   `json:"provider,omitempty"`
	Model      string   `json:"model,omitempty"`
	Workspace  string   `json:"workspace,omitempty"`
	Status     string   `json:"status,omitempty"`
	Transcript []string `json:"transcript,omitempty"`
	Pending    string   `json:"pending,omitempty"`
}

// SendState writes a display state without exposing backend capabilities.
func SendState(writer io.Writer, state State) error {
	return Write(writer, Message{Type: "state", Payload: mustJSON(state)})
}

func mustJSON(value any) json.RawMessage {
	encoded, _ := json.Marshal(value)
	return encoded
}

// Read decodes one bounded protocol message.
func Read(reader *bufio.Reader) (Message, error) {
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return Message{}, err
	}
	if len(line) > maxMessageBytes {
		return Message{}, fmt.Errorf("UI protocol message exceeds %d bytes", maxMessageBytes)
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
	if len(encoded) > maxMessageBytes {
		return fmt.Errorf("UI protocol message exceeds %d bytes", maxMessageBytes)
	}
	if _, err := writer.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("write UI protocol message: %w", err)
	}
	return nil
}
