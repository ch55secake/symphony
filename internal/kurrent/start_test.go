package kurrent

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type response struct {
	output string
	err    error
}

type fakeRunner struct {
	responses []response
	calls     [][]string
}

func (r *fakeRunner) run(_ context.Context, args ...string) ([]byte, error) {
	r.calls = append(r.calls, args)
	response := r.responses[0]
	r.responses = r.responses[1:]
	return []byte(response.output), response.err
}

func TestStartUsesHealthyRunningContainer(t *testing.T) {
	runner := &fakeRunner{responses: []response{{output: "true\n"}, {output: "healthy\n"}}}
	if err := start(context.Background(), runner, func(time.Duration) {}); err != nil {
		t.Fatalf("start() error = %v", err)
	}
	if len(runner.calls) != 2 || runner.calls[0][0] != "inspect" || runner.calls[1][0] != "inspect" {
		t.Fatalf("calls = %#v", runner.calls)
	}
}

func TestStartCreatesMissingContainer(t *testing.T) {
	runner := &fakeRunner{responses: []response{{err: &exec.ExitError{}}, {}, {output: "healthy"}}}
	if err := start(context.Background(), runner, func(time.Duration) {}); err != nil {
		t.Fatalf("start() error = %v", err)
	}
	if len(runner.calls) != 3 || runner.calls[1][0] != "run" || !strings.Contains(strings.Join(runner.calls[1], " "), "127.0.0.1:2113:2113") {
		t.Fatalf("calls = %#v", runner.calls)
	}
}

func TestStartReportsUnavailableDocker(t *testing.T) {
	runner := &fakeRunner{responses: []response{{err: errors.New("executable file not found")}}}
	if err := start(context.Background(), runner, func(time.Duration) {}); err == nil || !strings.Contains(err.Error(), "inspect KurrentDB") {
		t.Fatalf("start() error = %v", err)
	}
}
