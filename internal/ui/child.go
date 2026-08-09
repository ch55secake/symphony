package ui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

const shutdownGracePeriod = 750 * time.Millisecond
const shutdownRequestTimeout = 100 * time.Millisecond

// Child is an OpenTUI process connected over private file descriptors.
type Child struct {
	Command *exec.Cmd
	Reader  *bufio.Reader
	reader  io.Closer
	Writer  io.WriteCloser
	done    chan struct{}
	waitErr error
	close   sync.Once
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
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		return command.Process.Signal(syscall.SIGTERM)
	}
	command.WaitDelay = shutdownGracePeriod
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
	child := &Child{Command: command, Reader: bufio.NewReader(fromUIReader), reader: fromUIReader, Writer: toUIWriter, done: make(chan struct{})}
	go func() {
		child.waitErr = command.Wait()
		close(child.done)
	}()
	return child, nil
}

// Close asks the UI to restore the terminal before escalating to process signals.
func (c *Child) Close() error {
	if c == nil {
		return nil
	}
	c.close.Do(func() {
		if c.Writer != nil {
			requestDone := make(chan struct{})
			go func() {
				_ = Write(c.Writer, Message{Type: "app.shutdown"})
				_ = c.Writer.Close()
				close(requestDone)
			}()
			select {
			case <-requestDone:
			case <-time.After(shutdownRequestTimeout):
				_ = c.Writer.Close()
				<-requestDone
			}
		}
		if !c.wait(shutdownGracePeriod) && c.Command != nil && c.Command.Process != nil {
			_ = c.Command.Process.Signal(syscall.SIGTERM)
			if !c.wait(shutdownGracePeriod) {
				_ = c.Command.Process.Kill()
				<-c.done
			}
		}
		if c.reader != nil {
			_ = c.reader.Close()
		}
	})
	return c.waitErr
}

func (c *Child) wait(timeout time.Duration) bool {
	if c.done == nil {
		return true
	}
	select {
	case <-c.done:
		return true
	case <-time.After(timeout):
		return false
	}
}
