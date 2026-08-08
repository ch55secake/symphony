package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ch55secake/symphony/internal/audit"
	"github.com/ch55secake/symphony/internal/events"
	"github.com/ch55secake/symphony/internal/session"
)

func TestCommandRequiresApprovalAndPersistsSafeMetadata(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store, commands, handle := newService(t, root)
	command := Command{Executable: "sh", Arguments: []string{"-c", "touch marker"}}
	request, err := commands.RequestCommand(context.Background(), handle, "agent", command)
	if err != nil {
		t.Fatalf("RequestCommand() error = %v", err)
	}
	if _, err := commands.ExecuteCommand(context.Background(), handle, "agent", request, command); !errors.Is(err, ErrCommandApprovalRequired) {
		t.Fatalf("ExecuteCommand() error = %v, want ErrCommandApprovalRequired", err)
	}
	if _, err := os.Stat(filepath.Join(root, "marker")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unapproved command created marker: %v", err)
	}
	if err := commands.ApproveCommand(context.Background(), handle, "user", request); err != nil {
		t.Fatalf("ApproveCommand() error = %v", err)
	}
	if _, err := commands.ExecuteCommand(context.Background(), handle, "agent", request, command); err != nil {
		t.Fatalf("ExecuteCommand() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "marker")); err != nil {
		t.Fatalf("approved command did not create marker: %v", err)
	}
	if len(store.events) != 4 || store.events[1].Type != events.CommandRequested || store.events[2].Type != events.CommandApproved || store.events[3].Type != events.CommandCompleted {
		t.Fatalf("events = %#v, want command request, approval, and completion", store.events)
	}
}

func TestCommandRedactsArgumentsAndDoesNotPersistOutput(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store, commands, handle := newService(t, root)
	secret := "Bearer top-secret"
	command := Command{Executable: "sh", Arguments: []string{"-c", "printf %s \"$1\"", "sh", secret, "--token=abc123", "--token", "top-secret", "-secret", "also-secret"}}
	request, err := commands.RequestCommand(context.Background(), handle, "agent", command)
	if err != nil {
		t.Fatalf("RequestCommand() error = %v", err)
	}
	if err := commands.ApproveCommand(context.Background(), handle, "user", request); err != nil {
		t.Fatalf("ApproveCommand() error = %v", err)
	}
	result, err := commands.ExecuteCommand(context.Background(), handle, "agent", request, command)
	if err != nil {
		t.Fatalf("ExecuteCommand() error = %v", err)
	}
	if got := string(result.Stdout); got != secret {
		t.Fatalf("stdout = %q, want %q", got, secret)
	}
	encoded, err := json.Marshal(store.events)
	if err != nil {
		t.Fatalf("marshal events: %v", err)
	}
	if strings.Contains(string(encoded), secret) || strings.Contains(string(encoded), "--token=abc123") || strings.Contains(string(encoded), "top-secret") || strings.Contains(string(encoded), "also-secret") {
		t.Fatal("persisted events contain raw secret command data or output")
	}
}

func TestCommandRecordsFailures(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		command  Command
		execute  Command
		context  func() (context.Context, context.CancelFunc)
		code     string
		exitCode int
		want     error
	}{
		"changed command": {
			command:  Command{Executable: "sh", Arguments: []string{"-c", "true"}},
			execute:  Command{Executable: "sh", Arguments: []string{"-c", "false"}},
			context:  func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
			code:     "command_changed",
			exitCode: -1,
			want:     ErrCommandChanged,
		},
		"non-zero exit": {
			command:  Command{Executable: "sh", Arguments: []string{"-c", "exit 7"}},
			execute:  Command{Executable: "sh", Arguments: []string{"-c", "exit 7"}},
			context:  func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
			code:     "exit_nonzero",
			exitCode: 7,
		},
		"workspace escape": {
			command:  Command{Executable: "sh", Arguments: []string{"-c", "true"}, WorkingDirectory: "../"},
			execute:  Command{Executable: "sh", Arguments: []string{"-c", "true"}, WorkingDirectory: "../"},
			context:  func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
			code:     "path_outside_workspace",
			exitCode: -1,
			want:     ErrPathOutsideWorkspace,
		},
		"canceled": {
			command: Command{Executable: "sh", Arguments: []string{"-c", "while :; do :; done"}},
			execute: Command{Executable: "sh", Arguments: []string{"-c", "while :; do :; done"}},
			context: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 20*time.Millisecond)
			},
			code:     "canceled",
			exitCode: -1,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			store, commands, handle := newService(t, root)
			request, err := commands.RequestCommand(context.Background(), handle, "agent", test.command)
			if err != nil {
				t.Fatalf("RequestCommand() error = %v", err)
			}
			if err := commands.ApproveCommand(context.Background(), handle, "user", request); err != nil {
				t.Fatalf("ApproveCommand() error = %v", err)
			}
			ctx, cancel := test.context()
			defer cancel()
			result, err := commands.ExecuteCommand(ctx, handle, "agent", request, test.execute)
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("ExecuteCommand() error = %v, want %v", err, test.want)
			}
			if err == nil {
				t.Fatal("ExecuteCommand() error = nil, want failure")
			}
			if result.ExitCode != test.exitCode {
				t.Fatalf("exit code = %d, want %d", result.ExitCode, test.exitCode)
			}
			assertCommandFailure(t, store.events, test.code)
		})
	}
}

func TestOutputCaptureBoundsRuntimeOutput(t *testing.T) {
	t.Parallel()
	capture := outputCapture{}
	data := make([]byte, maxCommandOutputBytes+1)
	if _, err := capture.Write(data); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if len(capture.Bytes()) != maxCommandOutputBytes || !capture.truncated {
		t.Fatalf("capture = %d bytes, truncated %t", len(capture.Bytes()), capture.truncated)
	}
	if metadata := capture.metadata(); metadata.Bytes != int64(len(data)) || metadata.CapturedBytes != maxCommandOutputBytes || !metadata.Truncated {
		t.Fatalf("metadata = %#v, want bounded output metadata", metadata)
	} else if metadata.Hash != events.Hash(data) {
		t.Fatalf("metadata hash = %q, want complete output hash", metadata.Hash)
	}
}

func TestCommandRetriesOutcomePersistenceWithoutRerunning(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		command   Command
		eventType events.Type
		want      error
	}{
		"completion": {command: Command{Executable: "sh", Arguments: []string{"-c", "printf run >> marker"}}, eventType: events.CommandCompleted},
		"failure":    {command: Command{Executable: "sh", Arguments: []string{"-c", "printf run >> marker; exit 7"}}, eventType: events.CommandFailed},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			store := &flakyStore{failType: test.eventType, failures: 1}
			sessions := session.New(store, audit.DefaultPolicy())
			handle, err := sessions.Start(context.Background(), "user", root)
			if err != nil {
				t.Fatalf("Start() error = %v", err)
			}
			commands, err := New(sessions, root)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			request, err := commands.RequestCommand(context.Background(), handle, "agent", test.command)
			if err != nil {
				t.Fatalf("RequestCommand() error = %v", err)
			}
			if err := commands.ApproveCommand(context.Background(), handle, "user", request); err != nil {
				t.Fatalf("ApproveCommand() error = %v", err)
			}
			if _, err := commands.ExecuteCommand(context.Background(), handle, "agent", request, test.command); err == nil {
				t.Fatal("first ExecuteCommand() error = nil, want outcome persistence failure")
			}
			_, err = commands.ExecuteCommand(context.Background(), handle, "agent", request, test.command)
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("retry ExecuteCommand() error = %v, want %v", err, test.want)
			}
			if name == "completion" && err != nil {
				t.Fatalf("retry ExecuteCommand() error = %v", err)
			}
			if name == "failure" && err == nil {
				t.Fatal("retry ExecuteCommand() error = nil, want command failure")
			}
			if _, err := commands.ExecuteCommand(context.Background(), handle, "agent", request, test.command); !errors.Is(err, ErrCommandAlreadyExecuted) {
				t.Fatalf("third ExecuteCommand() error = %v, want ErrCommandAlreadyExecuted", err)
			}
			content, err := os.ReadFile(filepath.Join(root, "marker"))
			if err != nil || string(content) != "run" {
				t.Fatalf("marker = %q, %v; command reran", content, err)
			}
			if countEvents(store.events, test.eventType) != 1 {
				t.Fatalf("events = %#v, want one durable outcome", store.events)
			}
		})
	}
}

type flakyStore struct {
	recordingStore
	failType events.Type
	failures int
}

func (s *flakyStore) Append(ctx context.Context, event events.Event, expectedRevision *uint64) (uint64, error) {
	if event.Type == s.failType && s.failures > 0 {
		s.failures--
		return 0, errors.New("append failed")
	}
	return s.recordingStore.Append(ctx, event, expectedRevision)
}

func countEvents(recorded []events.Event, eventType events.Type) int {
	count := 0
	for _, event := range recorded {
		if event.Type == eventType {
			count++
		}
	}
	return count
}

func assertCommandFailure(t *testing.T, recorded []events.Event, wantCode string) {
	t.Helper()
	if len(recorded) != 4 || recorded[3].Type != events.CommandFailed {
		t.Fatalf("events = %#v, want failed command outcome", recorded)
	}
	var failure events.CommandFailedPayload
	if err := json.Unmarshal(recorded[3].Payload, &failure); err != nil {
		t.Fatalf("unmarshal failure: %v", err)
	}
	if failure.Code != wantCode {
		t.Fatalf("failure code = %q, want %q", failure.Code, wantCode)
	}
}
