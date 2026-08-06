package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/ch55secake/symphony/internal/events"
	"github.com/ch55secake/symphony/internal/session"
	"github.com/google/uuid"
)

const maxCommandOutputBytes = 1 << 20

var (
	ErrCommandApprovalRequired = errors.New("command approval is required")
	ErrCommandChanged          = errors.New("command does not match request")
	ErrCommandAlreadyApproved  = errors.New("command request is already approved")
	ErrCommandAlreadyExecuted  = errors.New("command request has already been executed")
	ErrCommandSessionMismatch  = errors.New("command request belongs to another session")
)

// Command is a shell-free process invocation rooted in the workspace.
type Command struct {
	Executable       string
	Arguments        []string
	WorkingDirectory string
}

// CommandRequest identifies a command that has been proposed for execution.
type CommandRequest struct {
	mu          sync.Mutex
	OperationID uuid.UUID
	sessionID   uuid.UUID
	commandHash string
	approved    bool
	executed    bool
}

// CommandResult returns bounded process output to the runtime without persistence.
type CommandResult struct {
	Stdout    []byte
	Stderr    []byte
	ExitCode  int
	Truncated bool
}

// RequestCommand persists command intent before it can be approved or executed.
func (s *Service) RequestCommand(ctx context.Context, handle *session.Handle, actor string, command Command) (*CommandRequest, error) {
	if handle == nil {
		return nil, fmt.Errorf("session handle is required")
	}
	if command.Executable == "" {
		return nil, fmt.Errorf("command executable is required")
	}
	commandHash, err := hashCommand(command)
	if err != nil {
		return nil, err
	}
	request := &CommandRequest{OperationID: uuid.New(), sessionID: handle.SessionID, commandHash: commandHash}
	if err := s.sessions.Record(ctx, handle, events.CommandRequested, actor, events.CommandRequestedPayload{
		OperationID:      request.OperationID.String(),
		Executable:       command.Executable,
		Arguments:        command.Arguments,
		WorkingDirectory: command.WorkingDirectory,
		CommandHash:      request.commandHash,
	}); err != nil {
		return nil, fmt.Errorf("record command intent: %w", err)
	}
	return request, nil
}

// ApproveCommand persists an approval before the requested command can execute.
func (s *Service) ApproveCommand(ctx context.Context, handle *session.Handle, actor string, request *CommandRequest) error {
	if request == nil {
		return fmt.Errorf("command request is required")
	}
	request.mu.Lock()
	defer request.mu.Unlock()
	if request.executed {
		return ErrCommandAlreadyExecuted
	}
	if err := request.validateHandle(handle); err != nil {
		return err
	}
	if request.approved {
		return ErrCommandAlreadyApproved
	}
	if err := s.sessions.Record(ctx, handle, events.CommandApproved, actor, events.CommandApprovedPayload{
		OperationID: request.OperationID.String(),
		CommandHash: request.commandHash,
	}); err != nil {
		return fmt.Errorf("record command approval: %w", err)
	}
	request.approved = true
	return nil
}

// ExecuteCommand runs an approved command when its invocation matches the request.
func (s *Service) ExecuteCommand(ctx context.Context, handle *session.Handle, actor string, request *CommandRequest, command Command) (CommandResult, error) {
	if request == nil {
		return CommandResult{}, fmt.Errorf("command request is required")
	}
	request.mu.Lock()
	defer request.mu.Unlock()
	if request.executed {
		return CommandResult{}, ErrCommandAlreadyExecuted
	}
	if err := request.validateHandle(handle); err != nil {
		return CommandResult{}, err
	}
	if !request.approved {
		return CommandResult{}, ErrCommandApprovalRequired
	}
	request.executed = true

	started := time.Now()
	commandHash, err := hashCommand(command)
	if err != nil || commandHash != request.commandHash {
		return CommandResult{ExitCode: -1}, s.recordCommandFailure(ctx, handle, actor, request, "command_changed", -1, outputCapture{}, outputCapture{}, started, ErrCommandChanged)
	}
	workingDirectory, err := s.commandDirectory(command.WorkingDirectory)
	if err != nil {
		return CommandResult{ExitCode: -1}, s.recordCommandFailure(ctx, handle, actor, request, commandErrorCode(err), -1, outputCapture{}, outputCapture{}, started, err)
	}

	stdout, stderr := outputCapture{}, outputCapture{}
	process := exec.CommandContext(ctx, command.Executable, command.Arguments...)
	process.Dir = workingDirectory
	process.Stdout = &stdout
	process.Stderr = &stderr
	err = process.Run()
	result := CommandResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: exitCode(err), Truncated: stdout.truncated || stderr.truncated}
	if err != nil {
		code := commandErrorCode(err)
		if ctx.Err() != nil {
			code = "canceled"
		}
		return result, s.recordCommandFailure(ctx, handle, actor, request, code, result.ExitCode, stdout, stderr, started, err)
	}
	if err := s.sessions.Record(ctx, handle, events.CommandCompleted, actor, events.CommandCompletedPayload{
		OperationID: request.OperationID.String(),
		CommandHash: request.commandHash,
		ExitCode:    result.ExitCode,
		Stdout:      stdout.metadata(),
		Stderr:      stderr.metadata(),
		DurationMS:  time.Since(started).Milliseconds(),
	}); err != nil {
		return result, fmt.Errorf("record command completion: %w", err)
	}
	return result, nil
}

func (r *CommandRequest) validateHandle(handle *session.Handle) error {
	if handle == nil {
		return fmt.Errorf("session handle is required")
	}
	if r.sessionID != handle.SessionID {
		return ErrCommandSessionMismatch
	}
	return nil
}

func (s *Service) recordCommandFailure(ctx context.Context, handle *session.Handle, actor string, request *CommandRequest, code string, exitCode int, stdout, stderr outputCapture, started time.Time, cause error) error {
	outcomeErr := s.sessions.Record(ctx, handle, events.CommandFailed, actor, events.CommandFailedPayload{
		OperationID: request.OperationID.String(),
		CommandHash: request.commandHash,
		Code:        code,
		ExitCode:    exitCode,
		Stdout:      stdout.metadata(),
		Stderr:      stderr.metadata(),
		DurationMS:  time.Since(started).Milliseconds(),
	})
	if outcomeErr != nil {
		return errors.Join(fmt.Errorf("run workspace command: %w", cause), fmt.Errorf("record command failure: %w", outcomeErr))
	}
	return fmt.Errorf("run workspace command: %w", cause)
}

func (s *Service) commandDirectory(workingDirectory string) (string, error) {
	if workingDirectory == "" {
		return s.root, nil
	}
	if filepath.IsAbs(workingDirectory) {
		return "", ErrPathOutsideWorkspace
	}
	candidate := filepath.Join(s.root, filepath.Clean(workingDirectory))
	if !within(s.root, candidate) {
		return "", ErrPathOutsideWorkspace
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", err
	}
	if !within(s.root, resolved) {
		return "", ErrPathOutsideWorkspace
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("command working directory is not a directory")
	}
	return resolved, nil
}

func hashCommand(command Command) (string, error) {
	encoded, err := json.Marshal(command)
	if err != nil {
		return "", fmt.Errorf("marshal command: %w", err)
	}
	return events.Hash(encoded), nil
}

func commandErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrPathOutsideWorkspace):
		return "path_outside_workspace"
	case errors.Is(err, fs.ErrNotExist):
		return "not_found"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "canceled"
	case errors.As(err, new(*exec.ExitError)):
		return "exit_nonzero"
	case errors.As(err, new(*exec.Error)):
		return "start_failed"
	default:
		return "command_failed"
	}
}

func exitCode(err error) int {
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode()
	}
	if err == nil {
		return 0
	}
	return -1
}

type outputCapture struct {
	buffer    bytes.Buffer
	bytes     int64
	truncated bool
}

func (c *outputCapture) Write(data []byte) (int, error) {
	c.bytes += int64(len(data))
	remaining := maxCommandOutputBytes - c.buffer.Len()
	if remaining > 0 {
		if len(data) > remaining {
			c.buffer.Write(data[:remaining])
			c.truncated = true
		} else {
			c.buffer.Write(data)
		}
	} else {
		c.truncated = true
	}
	return len(data), nil
}

func (c *outputCapture) Bytes() []byte {
	return c.buffer.Bytes()
}

func (c *outputCapture) metadata() events.CommandOutputMetadata {
	return events.CommandOutputMetadata{
		Bytes:         c.bytes,
		CapturedBytes: c.buffer.Len(),
		Hash:          events.Hash(c.buffer.Bytes()),
		Truncated:     c.truncated,
	}
}
