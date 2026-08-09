package ui

import (
	"bufio"
	"bytes"
	"encoding/json"
	"testing"
)

func TestWriteThenRead(t *testing.T) {
	var buffer bytes.Buffer
	if err := Write(&buffer, Message{Type: "app.ready"}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	message, err := Read(bufio.NewReader(&buffer))
	if err != nil || message.Type != "app.ready" || message.Version != Version {
		t.Fatalf("Read() = %#v, %v", message, err)
	}
}

func TestSendStateIncludesStructuredApproval(t *testing.T) {
	var buffer bytes.Buffer
	want := Approval{Action: "write_file", Summary: "write note.txt", Hash: "sha256:test"}
	if err := SendState(&buffer, State{Phase: "chat", Approval: &want}); err != nil {
		t.Fatalf("SendState() error = %v", err)
	}
	message, err := Read(bufio.NewReader(&buffer))
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	var state State
	if err := json.Unmarshal(message.Payload, &state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if state.Approval == nil || *state.Approval != want {
		t.Fatalf("approval = %#v, want %#v", state.Approval, want)
	}
}

func TestSendStateIncludesApprovalMode(t *testing.T) {
	var buffer bytes.Buffer
	if err := SendState(&buffer, State{Phase: "settings", AllowAll: true}); err != nil {
		t.Fatalf("SendState() error = %v", err)
	}
	message, err := Read(bufio.NewReader(&buffer))
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	var state State
	if err := json.Unmarshal(message.Payload, &state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if !state.AllowAll {
		t.Fatal("allow_all = false, want true")
	}
}
