package ui

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestChildCloseRequestsGracefulShutdown(t *testing.T) {
	executable := writeTestExecutable(t, `#!/bin/sh
IFS= read -r message <&3
case "$message" in
  *app.shutdown*) exit 0 ;;
  *) exit 2 ;;
esac
`)
	child, err := Start(context.Background(), executable)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	started := time.Now()
	if err := child.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed >= shutdownGracePeriod {
		t.Fatalf("Close() took %v, graceful shutdown was not used", elapsed)
	}
	if err := child.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestChildCloseSuppressesExpectedTerminationError(t *testing.T) {
	executable := writeTestExecutable(t, `#!/bin/sh
trap '' TERM
while :; do :; done
`)
	child, err := Start(context.Background(), executable)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := child.Close(); err != nil {
		t.Fatalf("Close() error = %v, want nil after intentional escalation", err)
	}
}

func writeTestExecutable(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ui-child")
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatalf("write test executable: %v", err)
	}
	return path
}
