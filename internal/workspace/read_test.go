package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ch55secake/symphony/internal/audit"
	"github.com/ch55secake/symphony/internal/events"
	"github.com/ch55secake/symphony/internal/session"
)

type recordingStore struct {
	events []events.Event
}

func (s *recordingStore) Append(_ context.Context, event events.Event, expectedRevision *uint64) (uint64, error) {
	s.events = append(s.events, event)
	if expectedRevision == nil {
		return 0, nil
	}
	return *expectedRevision + 1, nil
}

func TestReadRecordsIntentAndSafeCompletion(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	content := "Bearer top-secret"
	if err := os.WriteFile(filepath.Join(root, "secret.txt"), []byte(content), 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	store, reads, handle := newService(t, root)

	data, err := reads.Read(context.Background(), handle, "agent", "secret.txt")
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if string(data) != content {
		t.Fatalf("Read() data = %q, want %q", data, content)
	}
	if len(store.events) != 3 || store.events[1].Type != events.FileReadRequested || store.events[2].Type != events.FileReadCompleted {
		t.Fatalf("events = %#v, want start, read request, and read completion", store.events)
	}
	if store.events[2].CausationID == nil || *store.events[2].CausationID != store.events[1].ID {
		t.Fatal("read completion does not cite read intent as its cause")
	}
	var completion events.FileReadCompletedPayload
	if err := json.Unmarshal(store.events[2].Payload, &completion); err != nil {
		t.Fatalf("unmarshal completion payload: %v", err)
	}
	if completion.Bytes != len(content) || completion.ContentHash != events.Hash([]byte(content)) {
		t.Fatalf("completion = %#v, want safe file metadata", completion)
	}
	encoded, err := json.Marshal(store.events)
	if err != nil {
		t.Fatalf("marshal stored events: %v", err)
	}
	if strings.Contains(string(encoded), content) {
		t.Fatal("persisted events contain raw file content")
	}
}

func TestReadRecordsFailuresWithoutEscapingWorkspace(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		path  string
		setup func(t *testing.T, root string)
		code  string
	}{
		"path traversal": {path: "../outside.txt", code: "path_outside_workspace"},
		"missing file":   {path: "missing.txt", code: "not_found"},
		"symlink escape": {
			path: "escape.txt",
			code: "path_outside_workspace",
			setup: func(t *testing.T, root string) {
				t.Helper()
				external := filepath.Join(t.TempDir(), "outside.txt")
				if err := os.WriteFile(external, []byte("outside"), 0o600); err != nil {
					t.Fatalf("write external file: %v", err)
				}
				if err := os.Symlink(external, filepath.Join(root, "escape.txt")); err != nil {
					t.Fatalf("create escape symlink: %v", err)
				}
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if test.setup != nil {
				test.setup(t, root)
			}
			store, reads, handle := newService(t, root)
			_, err := reads.Read(context.Background(), handle, "agent", test.path)
			if err == nil {
				t.Fatal("Read() error = nil, want failure")
			}
			if name != "missing file" && !errors.Is(err, ErrPathOutsideWorkspace) {
				t.Fatalf("Read() error = %v, want ErrPathOutsideWorkspace", err)
			}
			if len(store.events) != 3 || store.events[1].Type != events.FileReadRequested || store.events[2].Type != events.FileReadFailed {
				t.Fatalf("events = %#v, want start, read request, and read failure", store.events)
			}
			var failure events.FileReadFailedPayload
			if err := json.Unmarshal(store.events[2].Payload, &failure); err != nil {
				t.Fatalf("unmarshal failure payload: %v", err)
			}
			if failure.Code != test.code {
				t.Fatalf("failure code = %q, want %q", failure.Code, test.code)
			}
		})
	}
}

func newService(t *testing.T, root string) (*recordingStore, *Service, *session.Handle) {
	t.Helper()
	store := &recordingStore{}
	sessions := session.New(store, audit.DefaultPolicy())
	handle, err := sessions.Start(context.Background(), "user", root)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	reads, err := New(sessions, root)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return store, reads, handle
}
