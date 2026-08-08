// Package kurrent starts Symphony's local KurrentDB container.
package kurrent

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const containerName = "symphony-kurrentdb"

type commandRunner interface {
	run(context.Context, ...string) ([]byte, error)
}

type dockerRunner struct{}

func (dockerRunner) run(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "docker", args...).CombinedOutput()
}

// Start ensures the local KurrentDB container is running and healthy.
func Start(ctx context.Context) error {
	return start(ctx, dockerRunner{}, time.Sleep)
}

func start(ctx context.Context, runner commandRunner, sleep func(time.Duration)) error {
	output, err := runner.run(ctx, "inspect", "--format", "{{.State.Running}}", containerName)
	if err == nil {
		if strings.TrimSpace(string(output)) != "true" {
			if _, err := runner.run(ctx, "start", containerName); err != nil {
				return fmt.Errorf("start KurrentDB container: %w", err)
			}
		}
	} else {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) {
			return fmt.Errorf("inspect KurrentDB container: %w", err)
		}
		if _, err := runner.run(ctx,
			"run", "-d", "--name", containerName,
			"-p", "127.0.0.1:2113:2113",
			"-e", "KURRENTDB_CLUSTER_SIZE=1",
			"-e", "KURRENTDB_RUN_PROJECTIONS=All",
			"-e", "KURRENTDB_START_STANDARD_PROJECTIONS=true",
			"-e", "KURRENTDB_NODE_PORT=2113",
			"-e", "KURRENTDB_INSECURE=true",
			"-e", "KURRENTDB_ENABLE_ATOM_PUB_OVER_HTTP=true",
			"--health-cmd", "curl --fail http://localhost:2113/health/live || exit 1",
			"--health-interval", "5s",
			"--health-timeout", "5s",
			"--health-retries", "24",
			"--health-start-period", "10s",
			"-v", "symphony-kurrentdb-data:/var/lib/kurrentdb",
			"-v", "symphony-kurrentdb-logs:/var/log/kurrentdb",
			"docker.kurrent.io/kurrent-latest/kurrentdb:25.0.1@sha256:3d80e962fffd7a61ffbe07c41b9500a74bbd43e11e7bbee7b160fd5575b0fdea"); err != nil {
			return fmt.Errorf("create KurrentDB container: %w", err)
		}
	}

	for range 48 {
		output, err := runner.run(ctx, "inspect", "--format", "{{.State.Health.Status}}", containerName)
		if err != nil {
			return fmt.Errorf("inspect KurrentDB health: %w", err)
		}
		if strings.TrimSpace(string(output)) == "healthy" {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		sleep(2500 * time.Millisecond)
	}
	return errors.New("KurrentDB container did not become healthy")
}
