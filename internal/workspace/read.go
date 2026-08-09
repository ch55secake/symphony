// Package workspace provides audited, workspace-confined filesystem operations.
package workspace

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ch55secake/symphony/internal/events"
	"github.com/ch55secake/symphony/internal/session"
)

var ErrPathOutsideWorkspace = errors.New("path is outside the workspace")

// Service records intent and outcome events for reads rooted at root.
type Service struct {
	sessions *session.Service
	root     string
}

func New(sessions *session.Service, root string) (*Service, error) {
	if sessions == nil {
		return nil, fmt.Errorf("session service is required")
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root: %w", err)
	}
	info, err := os.Stat(resolvedRoot)
	if err != nil {
		return nil, fmt.Errorf("stat workspace root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workspace root is not a directory")
	}
	return &Service{sessions: sessions, root: resolvedRoot}, nil
}

// Read records an intent event before reading path, then persists a safe outcome.
func (s *Service) Read(ctx context.Context, handle *session.Handle, actor, path string) ([]byte, error) {
	if err := s.sessions.Record(ctx, handle, events.FileReadRequested, actor, events.FileReadRequestedPayload{Path: path}); err != nil {
		return nil, fmt.Errorf("record file read intent: %w", err)
	}

	started := time.Now()
	data, err := s.read(path)
	duration := time.Since(started).Milliseconds()
	outcomeCtx, cancelOutcome := session.OutcomeContext(ctx)
	defer cancelOutcome()
	if err != nil {
		outcomeErr := s.sessions.Record(outcomeCtx, handle, events.FileReadFailed, actor, events.FileReadFailedPayload{
			Path:       path,
			Code:       errorCode(err),
			DurationMS: duration,
		})
		if outcomeErr != nil {
			return nil, errors.Join(fmt.Errorf("read workspace file: %w", err), fmt.Errorf("record file read failure: %w", outcomeErr))
		}
		return nil, fmt.Errorf("read workspace file: %w", err)
	}

	if err := s.sessions.Record(outcomeCtx, handle, events.FileReadCompleted, actor, events.FileReadCompletedPayload{
		Path:        path,
		Bytes:       len(data),
		ContentHash: events.Hash(data),
		DurationMS:  duration,
	}); err != nil {
		return nil, fmt.Errorf("record file read completion: %w", err)
	}
	return data, nil
}

func (s *Service) read(path string) ([]byte, error) {
	if filepath.IsAbs(path) {
		return nil, ErrPathOutsideWorkspace
	}
	candidate := filepath.Join(s.root, filepath.Clean(path))
	if !within(s.root, candidate) {
		return nil, ErrPathOutsideWorkspace
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return nil, err
	}
	if !within(s.root, resolved) {
		return nil, ErrPathOutsideWorkspace
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("path is not a regular file")
	}
	return os.ReadFile(resolved)
}

func within(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func errorCode(err error) string {
	switch {
	case errors.Is(err, ErrPathOutsideWorkspace):
		return "path_outside_workspace"
	case errors.Is(err, fs.ErrNotExist):
		return "not_found"
	default:
		return "read_failed"
	}
}
