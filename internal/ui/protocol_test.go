package ui

import (
	"bufio"
	"bytes"
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
