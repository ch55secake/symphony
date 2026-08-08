package workspace

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ch55secake/symphony/internal/events"
	"github.com/ch55secake/symphony/internal/session"
	"github.com/google/uuid"
)

var (
	ErrApprovalRequired       = errors.New("write approval is required")
	ErrContentChanged         = errors.New("write content does not match request")
	ErrAlreadyApproved        = errors.New("write request is already approved")
	ErrAlreadyExecuted        = errors.New("write request has already been executed")
	ErrInvalidWriteTarget     = errors.New("write target is not a regular file")
	ErrRequestSessionMismatch = errors.New("write request belongs to another session")
)

// WriteRequest identifies content that has been proposed for a workspace write.
type WriteRequest struct {
	mu          sync.Mutex
	OperationID uuid.UUID
	sessionID   uuid.UUID
	Path        string
	Bytes       int
	ContentHash string
	approved    bool
	executed    bool
}

// RequestWrite persists intent for content without persisting the content itself.
func (s *Service) RequestWrite(ctx context.Context, handle *session.Handle, actor, path string, content []byte) (*WriteRequest, error) {
	if handle == nil {
		return nil, fmt.Errorf("session handle is required")
	}
	request := &WriteRequest{
		OperationID: uuid.New(),
		sessionID:   handle.SessionID,
		Path:        path,
		Bytes:       len(content),
		ContentHash: events.Hash(content),
	}
	if err := s.sessions.Record(ctx, handle, events.FileWriteRequested, actor, events.FileWriteRequestedPayload{
		OperationID: request.OperationID.String(),
		Path:        request.Path,
		Bytes:       request.Bytes,
		ContentHash: request.ContentHash,
	}); err != nil {
		return nil, fmt.Errorf("record file write intent: %w", err)
	}
	return request, nil
}

// ApproveWrite persists an approval before the requested content can be written.
func (s *Service) ApproveWrite(ctx context.Context, handle *session.Handle, actor string, request *WriteRequest) error {
	if request == nil {
		return fmt.Errorf("write request is required")
	}
	request.mu.Lock()
	defer request.mu.Unlock()
	if request.executed {
		return ErrAlreadyExecuted
	}
	if err := request.validateHandle(handle); err != nil {
		return err
	}
	if request.approved {
		return ErrAlreadyApproved
	}
	if err := s.sessions.Record(ctx, handle, events.FileWriteApproved, actor, events.FileWriteApprovedPayload{
		OperationID: request.OperationID.String(),
	}); err != nil {
		return fmt.Errorf("record file write approval: %w", err)
	}
	request.approved = true
	return nil
}

// ExecuteWrite atomically writes content only when it matches an approved request.
func (s *Service) ExecuteWrite(ctx context.Context, handle *session.Handle, actor string, request *WriteRequest, content []byte) error {
	if request == nil {
		return fmt.Errorf("write request is required")
	}
	request.mu.Lock()
	defer request.mu.Unlock()
	if request.executed {
		return ErrAlreadyExecuted
	}
	if err := request.validateHandle(handle); err != nil {
		return err
	}
	if !request.approved {
		return ErrApprovalRequired
	}
	request.executed = true

	started := time.Now()
	if len(content) != request.Bytes || events.Hash(content) != request.ContentHash {
		return s.recordWriteFailure(ctx, handle, actor, request, "content_changed", started, ErrContentChanged)
	}
	if err := s.write(request.Path, content); err != nil {
		return s.recordWriteFailure(ctx, handle, actor, request, writeErrorCode(err), started, err)
	}
	if err := s.sessions.Record(ctx, handle, events.FileWriteCompleted, actor, events.FileWriteCompletedPayload{
		OperationID: request.OperationID.String(),
		Path:        request.Path,
		Bytes:       request.Bytes,
		ContentHash: request.ContentHash,
		DurationMS:  time.Since(started).Milliseconds(),
	}); err != nil {
		return fmt.Errorf("record file write completion: %w", err)
	}
	return nil
}

func (r *WriteRequest) validateHandle(handle *session.Handle) error {
	if handle == nil {
		return fmt.Errorf("session handle is required")
	}
	if r.sessionID != handle.SessionID {
		return ErrRequestSessionMismatch
	}
	return nil
}

func (s *Service) recordWriteFailure(ctx context.Context, handle *session.Handle, actor string, request *WriteRequest, code string, started time.Time, cause error) error {
	outcomeErr := s.sessions.Record(ctx, handle, events.FileWriteFailed, actor, events.FileWriteFailedPayload{
		OperationID: request.OperationID.String(),
		Path:        request.Path,
		Code:        code,
		DurationMS:  time.Since(started).Milliseconds(),
	})
	if outcomeErr != nil {
		return errors.Join(fmt.Errorf("write workspace file: %w", cause), fmt.Errorf("record file write failure: %w", outcomeErr))
	}
	return fmt.Errorf("write workspace file: %w", cause)
}

func (s *Service) write(path string, content []byte) error {
	target, mode, err := s.writeTarget(path)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".symphony-write-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, target)
}

func (s *Service) writeTarget(path string) (string, fs.FileMode, error) {
	if filepath.IsAbs(path) {
		return "", 0, ErrPathOutsideWorkspace
	}
	candidate := filepath.Join(s.root, filepath.Clean(path))
	if !within(s.root, candidate) {
		return "", 0, ErrPathOutsideWorkspace
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(candidate))
	if err != nil {
		return "", 0, err
	}
	if !within(s.root, parent) {
		return "", 0, ErrPathOutsideWorkspace
	}
	target := filepath.Join(parent, filepath.Base(candidate))
	info, err := os.Lstat(target)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return target, 0o600, nil
		}
		return "", 0, err
	}
	if !info.Mode().IsRegular() {
		return "", 0, ErrInvalidWriteTarget
	}
	return target, info.Mode().Perm(), nil
}

func writeErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrPathOutsideWorkspace):
		return "path_outside_workspace"
	case errors.Is(err, fs.ErrNotExist):
		return "not_found"
	case errors.Is(err, ErrInvalidWriteTarget):
		return "invalid_target"
	default:
		return "write_failed"
	}
}
