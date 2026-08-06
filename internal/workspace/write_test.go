package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ch55secake/symphony/internal/events"
)

func TestWriteRequiresApprovalAndPersistsSafeLifecycle(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	target := filepath.Join(root, "note.txt")
	if err := os.WriteFile(target, []byte("old"), 0o640); err != nil {
		t.Fatalf("write target: %v", err)
	}
	store, writes, handle := newService(t, root)
	content := []byte("Bearer top-secret")
	request, err := writes.RequestWrite(context.Background(), handle, "agent", "note.txt", content)
	if err != nil {
		t.Fatalf("RequestWrite() error = %v", err)
	}
	if err := writes.ExecuteWrite(context.Background(), handle, "agent", request, content); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("ExecuteWrite() error = %v, want ErrApprovalRequired", err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "old" {
		t.Fatalf("target after unapproved write = %q, %v; want old content", got, err)
	}

	if err := writes.ApproveWrite(context.Background(), handle, "user", request); err != nil {
		t.Fatalf("ApproveWrite() error = %v", err)
	}
	if err := writes.ExecuteWrite(context.Background(), handle, "agent", request, content); err != nil {
		t.Fatalf("ExecuteWrite() error = %v", err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != string(content) {
		t.Fatalf("target after approved write = %q, %v; want new content", got, err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("target mode = %o, want 640", got)
	}
	if len(store.events) != 4 || store.events[1].Type != events.FileWriteRequested || store.events[2].Type != events.FileWriteApproved || store.events[3].Type != events.FileWriteCompleted {
		t.Fatalf("events = %#v, want start, request, approval, and completion", store.events)
	}
	var requested events.FileWriteRequestedPayload
	var completed events.FileWriteCompletedPayload
	if err := json.Unmarshal(store.events[1].Payload, &requested); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if err := json.Unmarshal(store.events[3].Payload, &completed); err != nil {
		t.Fatalf("unmarshal completion: %v", err)
	}
	if requested.OperationID != request.OperationID.String() || completed.OperationID != request.OperationID.String() || completed.ContentHash != events.Hash(content) {
		t.Fatalf("write metadata does not identify the approved content")
	}
	encoded, err := json.Marshal(store.events)
	if err != nil {
		t.Fatalf("marshal stored events: %v", err)
	}
	if strings.Contains(string(encoded), string(content)) {
		t.Fatal("persisted events contain raw write content")
	}
}

func TestWriteRejectsChangedContentAndRecordsFailure(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	target := filepath.Join(root, "note.txt")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	store, writes, handle := newService(t, root)
	request, err := writes.RequestWrite(context.Background(), handle, "agent", "note.txt", []byte("approved"))
	if err != nil {
		t.Fatalf("RequestWrite() error = %v", err)
	}
	if err := writes.ApproveWrite(context.Background(), handle, "user", request); err != nil {
		t.Fatalf("ApproveWrite() error = %v", err)
	}
	if err := writes.ExecuteWrite(context.Background(), handle, "agent", request, []byte("changed")); !errors.Is(err, ErrContentChanged) {
		t.Fatalf("ExecuteWrite() error = %v, want ErrContentChanged", err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "old" {
		t.Fatalf("target after rejected write = %q, %v; want old content", got, err)
	}
	assertWriteFailureCode(t, store.events, "content_changed")
}

func TestWriteRejectsUnsafeTargets(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		path  string
		setup func(t *testing.T, root string)
		want  error
		code  string
	}{
		"path traversal": {path: "../outside.txt", want: ErrPathOutsideWorkspace, code: "path_outside_workspace"},
		"symlink target": {
			path: "link.txt",
			want: ErrInvalidWriteTarget,
			code: "invalid_target",
			setup: func(t *testing.T, root string) {
				t.Helper()
				external := filepath.Join(t.TempDir(), "outside.txt")
				if err := os.WriteFile(external, []byte("outside"), 0o600); err != nil {
					t.Fatalf("write external file: %v", err)
				}
				if err := os.Symlink(external, filepath.Join(root, "link.txt")); err != nil {
					t.Fatalf("create symlink: %v", err)
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
			store, writes, handle := newService(t, root)
			request, err := writes.RequestWrite(context.Background(), handle, "agent", test.path, []byte("new"))
			if err != nil {
				t.Fatalf("RequestWrite() error = %v", err)
			}
			if err := writes.ApproveWrite(context.Background(), handle, "user", request); err != nil {
				t.Fatalf("ApproveWrite() error = %v", err)
			}
			if err := writes.ExecuteWrite(context.Background(), handle, "agent", request, []byte("new")); !errors.Is(err, test.want) {
				t.Fatalf("ExecuteWrite() error = %v, want %v", err, test.want)
			}
			assertWriteFailureCode(t, store.events, test.code)
		})
	}
}

func assertWriteFailureCode(t *testing.T, recorded []events.Event, want string) {
	t.Helper()
	if len(recorded) != 4 || recorded[3].Type != events.FileWriteFailed {
		t.Fatalf("events = %#v, want a failed write outcome", recorded)
	}
	var failure events.FileWriteFailedPayload
	if err := json.Unmarshal(recorded[3].Payload, &failure); err != nil {
		t.Fatalf("unmarshal failure: %v", err)
	}
	if failure.Code != want {
		t.Fatalf("failure code = %q, want %q", failure.Code, want)
	}
}
