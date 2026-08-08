package ui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// Child is an OpenTUI process connected over private file descriptors.
type Child struct {
	Command *exec.Cmd
	Reader  *bufio.Reader
	Writer  io.WriteCloser
}

// Start launches executable without using the terminal's standard streams for RPC.
func Start(ctx context.Context, executable string) (*Child, error) {
	toUIReader, toUIWriter, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create UI input pipe: %w", err)
	}
	fromUIReader, fromUIWriter, err := os.Pipe()
	if err != nil {
		_ = toUIReader.Close()
		_ = toUIWriter.Close()
		return nil, fmt.Errorf("create UI output pipe: %w", err)
	}
	command := exec.CommandContext(ctx, executable)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.ExtraFiles = []*os.File{toUIReader, fromUIWriter}
	command.Env = append(os.Environ(), "SYMPHONY_RPC_IN_FD=3", "SYMPHONY_RPC_OUT_FD=4")
	if err := command.Start(); err != nil {
		_ = toUIReader.Close()
		_ = toUIWriter.Close()
		_ = fromUIReader.Close()
		_ = fromUIWriter.Close()
		return nil, fmt.Errorf("start OpenTUI child: %w", err)
	}
	_ = toUIReader.Close()
	_ = fromUIWriter.Close()
	return &Child{Command: command, Reader: bufio.NewReader(fromUIReader), Writer: toUIWriter}, nil
}

// Close terminates the UI process and its private protocol pipes.
func (c *Child) Close() error {
	if c == nil {
		return nil
	}
	if c.Writer != nil {
		_ = c.Writer.Close()
	}
	if c.Command != nil && c.Command.Process != nil {
		_ = c.Command.Process.Kill()
	}
	if c.Command != nil {
		return c.Command.Wait()
	}
	return nil
}
