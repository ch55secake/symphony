// Package tui retains migration-only contracts while OpenTUI owns rendering.
package tui

import (
	"context"
	"errors"

	"github.com/ch55secake/symphony/internal/agent"
)

var ErrCanceled = errors.New("legacy Go TUI is unavailable")

type Config struct {
	Provider, Model, Workspace, SessionID, InitialPrompt string
}

type SetupConfig struct {
	Provider, Model, Workspace, APIKey string
}

type Runner interface {
	Turn(context.Context, []agent.Message) (agent.LoopResult, error)
	Resolve(context.Context, *agent.PendingApproval, bool) (agent.LoopResult, error)
}

type ModelLister func(context.Context, string, string) ([]string, error)

func WaitForKurrent(context.Context, func(context.Context) error) error { return ErrCanceled }
func Select(context.Context, SetupConfig, ModelLister) (SetupConfig, error) { return SetupConfig{}, ErrCanceled }
func Welcome(context.Context, SetupConfig) (string, error) { return "", ErrCanceled }
func Run(context.Context, Config, Runner) error { return ErrCanceled }
func SetTheme(string) error { return ErrCanceled }
