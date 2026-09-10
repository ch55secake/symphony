package providers

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const maxSSELineBytes = 1 << 20

// SSEEvent is one bounded server-sent event frame.
type SSEEvent struct {
	Name string
	Data []byte
}

// ReadSSE reads one event, ignoring comments and supporting multi-line data fields.
func ReadSSE(reader *bufio.Reader) (SSEEvent, error) {
	var name string
	var data []string
	for {
		line, err := readSSELine(reader)
		if err != nil {
			if errors.Is(err, io.EOF) && (name != "" || len(data) > 0) {
				return SSEEvent{Name: name, Data: []byte(strings.Join(data, "\n"))}, nil
			}
			return SSEEvent{}, err
		}
		if line == "" {
			if name == "" && len(data) == 0 {
				continue
			}
			return SSEEvent{Name: name, Data: []byte(strings.Join(data, "\n"))}, nil
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, found := strings.Cut(line, ":")
		if found {
			value = strings.TrimPrefix(value, " ")
		}
		switch field {
		case "event":
			name = value
		case "data":
			data = append(data, value)
		}
	}
}

func readSSELine(reader *bufio.Reader) (string, error) {
	var line []byte
	for {
		fragment, err := reader.ReadSlice('\n')
		if len(line)+len(fragment) > maxSSELineBytes {
			return "", fmt.Errorf("SSE line exceeds %d bytes", maxSSELineBytes)
		}
		line = append(line, fragment...)
		if err == nil {
			return strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r"), nil
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			if errors.Is(err, io.EOF) && len(line) > 0 {
				return strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r"), nil
			}
			return "", err
		}
	}
}

func DecodeSSE[T any](event SSEEvent, target *T) error {
	if len(event.Data) == 0 || string(event.Data) == "[DONE]" {
		return nil
	}
	if err := json.Unmarshal(event.Data, target); err != nil {
		return fmt.Errorf("decode SSE %s event: %w", event.Name, err)
	}
	return nil
}
