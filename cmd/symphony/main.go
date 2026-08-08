// Symphony runs an audited coding-agent session from the terminal.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/ch55secake/symphony/internal/agent"
	"github.com/ch55secake/symphony/internal/audit"
	"github.com/ch55secake/symphony/internal/events"
	"github.com/ch55secake/symphony/internal/providers/anthropic"
	"github.com/ch55secake/symphony/internal/providers/openai"
	"github.com/ch55secake/symphony/internal/session"
	"github.com/ch55secake/symphony/internal/store/kurrentdb"
	"github.com/ch55secake/symphony/internal/workspace"
	"github.com/google/uuid"
)

const actor = "cli"

type config struct {
	provider  string
	model     string
	workspace string
	prompt    string
}

type runtime struct {
	sessions *session.Service
	loop     *agent.Loop
	provider agent.Provider
	tools    []agent.ToolDefinition
	close    func() error
}

type runtimeFactory func(config) (*runtime, error)

type replayReader interface {
	Read(context.Context, uuid.UUID) ([]events.Event, error)
	Close() error
}

type replayFactory func(string) (replayReader, error)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := execute(ctx, os.Args[1:], os.Stdin, os.Stdout, newRuntime, newReplayReader); err != nil {
		fmt.Fprintln(os.Stderr, "symphony:", err)
		os.Exit(1)
	}
}

func execute(ctx context.Context, args []string, input io.Reader, output io.Writer, factory runtimeFactory, replayFactory replayFactory) error {
	if len(args) == 0 {
		return errors.New("usage: symphony run|replay")
	}
	switch args[0] {
	case "run":
		return run(ctx, args, input, output, factory)
	case "replay":
		return replay(ctx, args[1:], output, replayFactory)
	default:
		return errors.New("usage: symphony run|replay")
	}
}

func run(ctx context.Context, args []string, input io.Reader, output io.Writer, factory runtimeFactory) error {
	config, err := parseConfig(args)
	if err != nil {
		return err
	}
	runtime, err := factory(config)
	if err != nil {
		return err
	}
	defer runtime.close()

	handle, err := runtime.sessions.Start(ctx, actor, config.workspace)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "Session: %s\n", handle.SessionID); err != nil {
		_ = runtime.sessions.Fail(context.WithoutCancel(ctx), handle, actor, "output_failed")
		return err
	}
	result, err := runtime.loop.RunWithApproval(ctx, handle, actor, runtime.provider, agent.CompletionRequest{
		Model:    config.model,
		Messages: []agent.Message{{Role: agent.RoleUser, Content: config.prompt}},
		Tools:    runtime.tools,
	})
	for err == nil && result.Pending != nil {
		approved, promptErr := promptApproval(input, output, result.Pending)
		if promptErr != nil {
			err = promptErr
			break
		}
		if approved {
			result, err = runtime.loop.Approve(ctx, handle, actor, runtime.provider, result.Pending)
		} else {
			result, err = runtime.loop.Deny(ctx, handle, actor, runtime.provider, result.Pending, "user_denied")
		}
	}
	if err != nil {
		_ = runtime.sessions.Fail(context.WithoutCancel(ctx), handle, actor, failureReason(ctx))
		return err
	}
	if result.Completion == nil {
		_ = runtime.sessions.Fail(context.WithoutCancel(ctx), handle, actor, "runtime_failed")
		return errors.New("agent loop returned no completion")
	}
	if err := runtime.sessions.Finish(ctx, handle, actor, "completed"); err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, result.Completion.Content)
	return err
}

func replay(ctx context.Context, args []string, output io.Writer, factory replayFactory) error {
	if len(args) != 1 {
		return errors.New("usage: symphony replay SESSION_ID")
	}
	sessionID, err := uuid.Parse(args[0])
	if err != nil {
		return errors.New("session ID must be a UUID")
	}
	connectionString := os.Getenv("KURRENTDB_URL")
	if connectionString == "" {
		return errors.New("KURRENTDB_URL is required")
	}
	reader, err := factory(connectionString)
	if err != nil {
		return err
	}
	defer reader.Close()
	recorded, err := reader.Read(ctx, sessionID)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(output)
	for _, event := range recorded {
		if err := encoder.Encode(event); err != nil {
			return err
		}
	}
	return nil
}

func parseConfig(args []string) (config, error) {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	provider := flags.String("provider", "", "provider: openai or anthropic")
	model := flags.String("model", "", "model name")
	workspaceRoot := flags.String("workspace", "", "workspace root")
	if len(args) == 0 || args[0] != "run" {
		return config{}, errors.New("usage: symphony run --provider PROVIDER --model MODEL [--workspace PATH] PROMPT")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return config{}, err
	}
	if flags.NArg() != 1 || strings.TrimSpace(flags.Arg(0)) == "" {
		return config{}, errors.New("a prompt is required")
	}
	if *provider != "openai" && *provider != "anthropic" {
		return config{}, errors.New("provider must be openai or anthropic")
	}
	if strings.TrimSpace(*model) == "" {
		return config{}, errors.New("model is required")
	}
	root := *workspaceRoot
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return config{}, fmt.Errorf("get working directory: %w", err)
		}
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return config{}, fmt.Errorf("resolve workspace path: %w", err)
	}
	return config{provider: *provider, model: *model, workspace: root, prompt: flags.Arg(0)}, nil
}

func promptApproval(input io.Reader, output io.Writer, pending *agent.PendingApproval) (bool, error) {
	if _, err := fmt.Fprintf(output, "Approval required: %s\nHash: %s\nApprove? [y/N]: ", pending.Summary, pending.Hash); err != nil {
		return false, err
	}
	line, err := bufio.NewReader(input).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(line), "y") || strings.EqualFold(strings.TrimSpace(line), "yes"), nil
}

func failureReason(ctx context.Context) string {
	if ctx.Err() != nil {
		return "canceled"
	}
	return "runtime_failed"
}

func newRuntime(config config) (*runtime, error) {
	connectionString := os.Getenv("KURRENTDB_URL")
	if connectionString == "" {
		return nil, errors.New("KURRENTDB_URL is required")
	}
	store, err := kurrentdb.New(connectionString)
	if err != nil {
		return nil, err
	}
	provider, err := newProvider(config.provider)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	sessions := session.New(store, audit.DefaultPolicy())
	turns, err := agent.New(sessions)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	workspaceService, err := workspace.New(sessions, config.workspace)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	readTool, err := agent.NewReadFileTool(workspaceService, 64<<10)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	writeTool, err := agent.NewWriteFileTool(workspaceService)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	commandTool, err := agent.NewRunCommandTool(workspaceService)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	loop, err := agent.NewLoop(turns, []agent.Tool{readTool, writeTool, commandTool}, 8)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	return &runtime{
		sessions: sessions,
		loop:     loop,
		provider: provider,
		tools:    []agent.ToolDefinition{readTool.Definition(), writeTool.Definition(), commandTool.Definition()},
		close:    store.Close,
	}, nil
}

func newReplayReader(connectionString string) (replayReader, error) {
	return kurrentdb.New(connectionString)
}

func newProvider(name string) (agent.Provider, error) {
	switch name {
	case "openai":
		return openai.New(openai.Config{APIKey: os.Getenv("OPENAI_API_KEY")})
	case "anthropic":
		return anthropic.New(anthropic.Config{APIKey: os.Getenv("ANTHROPIC_API_KEY")})
	default:
		return nil, errors.New("provider must be openai or anthropic")
	}
}
