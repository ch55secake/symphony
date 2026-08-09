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
	"time"

	"github.com/ch55secake/symphony/internal/agent"
	"github.com/ch55secake/symphony/internal/audit"
	appconfig "github.com/ch55secake/symphony/internal/config"
	"github.com/ch55secake/symphony/internal/events"
	"github.com/ch55secake/symphony/internal/kurrent"
	"github.com/ch55secake/symphony/internal/models"
	"github.com/ch55secake/symphony/internal/providers/anthropic"
	"github.com/ch55secake/symphony/internal/providers/openai"
	"github.com/ch55secake/symphony/internal/providers/opencode"
	"github.com/ch55secake/symphony/internal/session"
	"github.com/ch55secake/symphony/internal/store/kurrentdb"
	"github.com/ch55secake/symphony/internal/tui"
	"github.com/ch55secake/symphony/internal/ui"
	"github.com/ch55secake/symphony/internal/workspace"
	"github.com/google/uuid"
)

const actor = "cli"

const localKurrentDBURL = "kurrentdb://localhost:2113?tls=false"

const uiOperationShutdownTimeout = 5 * time.Second

type config struct {
	provider         string
	transport        string
	model            string
	workspace        string
	prompt           string
	connectionString string
	apiKey           string
}

type uiToolActivity struct {
	Activity      agent.ToolActivity
	AfterMessages int
	SourceID      string
}

type runtime struct {
	sessions *session.Service
	loop     *agent.Loop
	provider agent.Provider
	tools    []agent.ToolDefinition
	close    func() error
}

type runtimeFactory func(config) (*runtime, error)

type kurrentStarter func(context.Context) error

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
		return runTUI(ctx, factory, kurrent.Start)
	}
	switch args[0] {
	case "run":
		return run(ctx, args, input, output, factory)
	case "tui":
		if len(args) != 1 {
			return errors.New("TUI settings are configured interactively")
		}
		return runTUI(ctx, factory, kurrent.Start)
	case "replay":
		return replay(ctx, args[1:], output, replayFactory)
	default:
		return errors.New("usage: symphony [tui|run|replay]")
	}
}

func runTUI(ctx context.Context, factory runtimeFactory, startKurrent kurrentStarter) error {
	executable := strings.TrimSpace(os.Getenv("SYMPHONY_UI_EXECUTABLE"))
	if executable == "" {
		var err error
		executable, err = ui.Extract()
		if err != nil {
			return runBubbleTUI(ctx, factory, startKurrent)
		}
	}
	return runOpenTUI(ctx, factory, startKurrent, executable)
}

func runBubbleTUI(ctx context.Context, factory runtimeFactory, startKurrent kurrentStarter) error {
	workspace, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	workspace, err = filepath.Abs(workspace)
	if err != nil {
		return fmt.Errorf("resolve workspace path: %w", err)
	}
	if err := tui.WaitForKurrent(ctx, startKurrent); err != nil {
		if errors.Is(err, tui.ErrCanceled) {
			return nil
		}
		return err
	}
	settings, err := appconfig.Load()
	if err != nil {
		return err
	}
	theme := settings.Theme
	if theme == "" {
		theme = "default"
	}
	if err := tui.SetTheme(theme); err != nil {
		return err
	}
	selected, saved := savedSetup(settings, workspace)
	var config config
	if saved {
		config, err = configFromTUI(selected, settings)
	}
	if !saved || err != nil {
		selected, err = tui.Select(ctx, tui.SetupConfig{
			Provider:  settings.Provider,
			Model:     settings.Model,
			Workspace: workspace,
			APIKey:    providerAPIKey(settings.Provider, settings),
		}, func(ctx context.Context, provider, apiKey string) ([]string, error) {
			return models.List(ctx, models.Config{Provider: provider, APIKey: apiKey})
		})
		if errors.Is(err, tui.ErrCanceled) {
			return nil
		}
		if err != nil {
			return err
		}
		config, err = configFromTUI(selected, settings)
		if err != nil {
			return err
		}
		if err := appconfig.SaveConnection(selected.Provider, selected.APIKey, selected.Model); err != nil {
			return err
		}
	}
	initialPrompt, err := tui.Welcome(ctx, tui.SetupConfig{Provider: config.provider, Model: config.model, Workspace: config.workspace})
	if err != nil {
		if errors.Is(err, tui.ErrCanceled) {
			return nil
		}
		return err
	}
	runtime, err := factory(config)
	if err != nil {
		return err
	}
	defer func() { _ = runtime.close() }()

	handle, err := runtime.sessions.Start(ctx, actor, config.workspace)
	if err != nil {
		return err
	}
	err = tui.Run(ctx, tui.Config{
		Provider:      config.provider,
		Model:         config.model,
		Workspace:     config.workspace,
		SessionID:     handle.SessionID.String(),
		InitialPrompt: initialPrompt,
	}, &tuiRunner{runtime: runtime, handle: handle, config: config})
	if err != nil {
		reason := failureReason(ctx)
		if errors.Is(err, tui.ErrCanceled) {
			reason = "canceled"
		}
		_ = runtime.sessions.Fail(context.WithoutCancel(ctx), handle, actor, reason)
		return err
	}
	return runtime.sessions.Finish(ctx, handle, actor, "quit")
}

func runOpenTUI(ctx context.Context, factory runtimeFactory, startKurrent kurrentStarter, executable string) error {
	child, err := ui.Start(ctx, executable)
	if err != nil {
		return err
	}
	var appRuntime *runtime
	defer func() {
		_ = child.Close()
		if appRuntime != nil {
			_ = appRuntime.close()
		}
	}()
	if err := ui.SendState(child.Writer, ui.State{Phase: "starting", Status: "Starting local KurrentDB..."}); err != nil {
		return err
	}
	if err := startKurrent(ctx); err != nil {
		_ = ui.SendState(child.Writer, ui.State{Phase: "error", Status: err.Error()})
		return err
	}
	settings, err := appconfig.Load()
	if err != nil {
		return err
	}
	workspace, err := currentWorkspace()
	if err != nil {
		return err
	}
	selected, saved := savedSetup(settings, workspace)
	if !saved {
		return errors.New("OpenTUI setup requires a saved connection; run the Go TUI once or configure provider, model, and API key")
	}
	config, err := configFromTUI(selected, settings)
	if err != nil {
		return err
	}
	runtime, err := factory(config)
	if err != nil {
		return err
	}
	appRuntime = runtime
	handle, err := runtime.sessions.Start(ctx, actor, config.workspace)
	if err != nil {
		return err
	}
	messages := []agent.Message{}
	activities := []uiToolActivity{}
	var pending *agent.PendingApproval
	allowAll := false
	if err := ui.SendState(child.Writer, ui.State{Phase: "welcome", Provider: config.provider, Model: config.model, Theme: activeTheme(), Workspace: config.workspace, Status: "Enter starts chat"}); err != nil {
		return err
	}
	stateUpdates := make(chan ui.State, 1)
	stateWriteErrors := make(chan error, 1)
	stateWriterCtx, stopStateWriter := context.WithCancel(ctx)
	defer stopStateWriter()
	go func() {
		for {
			select {
			case state := <-stateUpdates:
				if err := ui.SendState(child.Writer, state); err != nil {
					stateWriteErrors <- err
					return
				}
			case <-stateWriterCtx.Done():
				return
			}
		}
	}()
	sendState := func(state ui.State) error {
		select {
		case err := <-stateWriteErrors:
			return err
		default:
		}
		select {
		case stateUpdates <- state:
		default:
			select {
			case <-stateUpdates:
			default:
			}
			select {
			case stateUpdates <- state:
			default:
			}
		}
		return nil
	}
	type readResult struct {
		message ui.Message
		err     error
	}
	type operationResult struct {
		result agent.LoopResult
		err    error
	}
	activityUpdates := make(chan uiToolActivity, 256)
	reads := make(chan readResult, 1)
	readCtx, stopReading := context.WithCancel(ctx)
	defer stopReading()
	go func() {
		for {
			message, err := ui.Read(child.Reader)
			select {
			case reads <- readResult{message: message, err: err}:
			case <-readCtx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	var operation <-chan operationResult
	var cancelOperation context.CancelFunc
	startOperation := func(run func(context.Context) (agent.LoopResult, error)) {
		operationCtx, cancel := context.WithCancel(ctx)
		results := make(chan operationResult, 1)
		operation = results
		cancelOperation = cancel
		go func() {
			result, err := run(operationCtx)
			results <- operationResult{result: result, err: err}
		}()
	}
	cancelAndWait := func() bool {
		if cancelOperation == nil {
			return true
		}
		cancelOperation()
		select {
		case <-operation:
			cancelOperation()
			operation = nil
			cancelOperation = nil
			return true
		case <-time.After(uiOperationShutdownTimeout):
			return false
		}
	}

	for {
		select {
		case err := <-stateWriteErrors:
			if !cancelAndWait() {
				appRuntime = nil
			}
			return err
		case <-ctx.Done():
			if !cancelAndWait() {
				appRuntime = nil
				return fmt.Errorf("timed out canceling UI operation: %w", ctx.Err())
			}
			outcomeCtx, cancelOutcome := session.OutcomeContext(ctx)
			_ = runtime.sessions.Fail(outcomeCtx, handle, actor, "canceled")
			cancelOutcome()
			return ctx.Err()
		case completed := <-operation:
			for draining := true; draining; {
				select {
				case update := <-activityUpdates:
					activities = upsertUIActivity(activities, update)
				default:
					draining = false
				}
			}
			operation = nil
			cancelOperation()
			cancelOperation = nil
			if completed.err != nil {
				if len(completed.result.Messages) > 0 {
					messages = completed.result.Messages
					activities = alignUIActivities(messages, activities)
				}
				if pending != nil && pending.Used() {
					pending = nil
				}
				status := displayUIError(completed.err)
				if errors.Is(completed.err, context.Canceled) {
					status = "Canceled"
				}
				if err := sendUIState(sendState, config, messages, activities, pending, status); err != nil {
					return err
				}
				continue
			}
			messages, pending = completed.result.Messages, completed.result.Pending
			activities = alignUIActivities(messages, activities)
			if err := sendUIState(sendState, config, messages, activities, pending, "READY"); err != nil {
				return err
			}
			continue
		case update := <-activityUpdates:
			activities = upsertUIActivity(activities, update)
			if err := sendUIState(sendState, config, messages, activities, pending, "WORKING"); err != nil {
				return err
			}
			continue
		case incoming := <-reads:
			if incoming.err != nil {
				if !cancelAndWait() {
					appRuntime = nil
					return fmt.Errorf("timed out stopping disconnected UI operation: %w", incoming.err)
				}
				outcomeCtx, cancelOutcome := session.OutcomeContext(ctx)
				_ = runtime.sessions.Fail(outcomeCtx, handle, actor, "ui_disconnected")
				cancelOutcome()
				return incoming.err
			}
			message := incoming.message
			switch message.Type {
			case "app.ready":
				if err := sendState(ui.State{Phase: "welcome", Provider: config.provider, Model: config.model, Theme: activeTheme(), Workspace: config.workspace, Status: "Enter starts chat"}); err != nil {
					return err
				}
				continue
			case "chat.start":
				if operation != nil {
					continue
				}
				if err := sendUIState(sendState, config, messages, activities, pending, "READY"); err != nil {
					return err
				}
				continue
			case "app.quit":
				if cancelOperation != nil {
					cancelOperation()
					_ = child.Close()
					if !cancelAndWait() {
						appRuntime = nil
						return errors.New("timed out waiting for the active operation to stop")
					}
				}
				finishCtx, cancelFinish := session.OutcomeContext(ctx)
				defer cancelFinish()
				return runtime.sessions.Finish(finishCtx, handle, actor, "quit")
			case "app.cancel":
				if cancelOperation == nil {
					finishCtx, cancelFinish := session.OutcomeContext(ctx)
					defer cancelFinish()
					return runtime.sessions.Finish(finishCtx, handle, actor, "quit")
				}
				cancelOperation()
				continue
			case "prompt.submit":
				var request struct {
					Prompt string `json:"prompt"`
				}
				if err := json.Unmarshal(message.Payload, &request); err != nil || strings.TrimSpace(request.Prompt) == "" || pending != nil || operation != nil {
					continue
				}
				command := strings.TrimSpace(request.Prompt)
				switch command {
				case "/model":
					available, err := listUIModels(ctx, runtime, handle, config)
					if err != nil {
						_ = sendUIState(sendState, config, messages, activities, pending, displayUIError(err))
						continue
					}
					if err := sendState(ui.State{Phase: "select", Selection: "model", Options: available, Provider: config.provider, Model: config.model, Theme: activeTheme(), Workspace: config.workspace, Status: "Select a model"}); err != nil {
						return err
					}
					continue
				case "/theme":
					if err := sendState(ui.State{Phase: "select", Selection: "theme", Options: []string{"default", "contrast", "mono"}, Provider: config.provider, Model: config.model, Theme: activeTheme(), Workspace: config.workspace, Status: "Select a theme"}); err != nil {
						return err
					}
					continue
				case "/allow-all", "/allow-all on":
					if err := sendState(ui.State{Phase: "confirm", Selection: "allow-all", Provider: config.provider, Model: config.model, Theme: activeTheme(), Workspace: config.workspace, Status: "Allow all workspace writes and commands for this session?"}); err != nil {
						return err
					}
					continue
				case "/settings":
					if err := sendState(ui.State{Phase: "settings", Provider: config.provider, Model: config.model, Theme: activeTheme(), Workspace: config.workspace, Status: "[/model] model  [/theme] theme  [/allow-all] approval"}); err != nil {
						return err
					}
					continue
				}
				if strings.HasPrefix(command, "/") {
					status, updatedAllowAll, handled := handleUICommand(ctx, runtime, handle, &config, request.Prompt, allowAll)
					if handled {
						allowAll = updatedAllowAll
						if err := sendUIState(sendState, config, messages, activities, pending, status); err != nil {
							return err
						}
						continue
					}
				}
				messages = append(messages, agent.Message{Role: agent.RoleUser, Content: request.Prompt})
				if err := sendUIState(sendState, config, messages, activities, nil, "WORKING"); err != nil {
					return err
				}
				requestMessages := append([]agent.Message(nil), messages...)
				afterMessages := len(messages)
				startOperation(func(operationCtx context.Context) (agent.LoopResult, error) {
					observer := func(activity agent.ToolActivity) {
						select {
						case activityUpdates <- uiToolActivity{Activity: activity, AfterMessages: afterMessages, SourceID: activity.ID}:
						default:
						}
					}
					result, err := runtime.loop.RunWithApprovalObserved(operationCtx, handle, actor, runtime.provider, agent.CompletionRequest{Model: config.model, Messages: requestMessages, Tools: runtime.tools}, observer)
					for err == nil && result.Pending != nil && allowAll {
						result, err = runtime.loop.ApproveObserved(operationCtx, handle, actor, runtime.provider, result.Pending, observer)
					}
					return result, err
				})
				continue
			case "selection.submit":
				if operation != nil {
					continue
				}
				var request struct {
					Selection string `json:"selection"`
					Value     string `json:"value"`
				}
				if err := json.Unmarshal(message.Payload, &request); err != nil || request.Value == "" {
					continue
				}
				switch request.Selection {
				case "model", "theme":
				default:
					continue
				}
				command := "/" + request.Selection + " " + request.Value
				status, updatedAllowAll, _ := handleUICommand(ctx, runtime, handle, &config, command, allowAll)
				allowAll = updatedAllowAll
				if err := sendUIState(sendState, config, messages, activities, pending, status); err != nil {
					return err
				}
				continue
			case "allow-all.confirm":
				if operation != nil {
					continue
				}
				var request struct {
					Approved bool `json:"approved"`
				}
				if err := json.Unmarshal(message.Payload, &request); err != nil || !request.Approved {
					if err := sendUIState(sendState, config, messages, activities, pending, "Allow all canceled"); err != nil {
						return err
					}
					continue
				}
				status, updatedAllowAll, _ := handleUICommand(ctx, runtime, handle, &config, "/allow-all on", allowAll)
				allowAll = updatedAllowAll
				if err := sendUIState(sendState, config, messages, activities, pending, status); err != nil {
					return err
				}
				continue
			case "approval.resolve":
				var request struct {
					Approved bool `json:"approved"`
				}
				if err := json.Unmarshal(message.Payload, &request); err != nil || pending == nil || operation != nil {
					continue
				}
				approval := pending
				pending = nil
				if err := sendUIState(sendState, config, messages, activities, nil, "WORKING"); err != nil {
					return err
				}
				afterMessages := len(messages)
				startOperation(func(operationCtx context.Context) (agent.LoopResult, error) {
					observer := func(activity agent.ToolActivity) {
						select {
						case activityUpdates <- uiToolActivity{Activity: activity, AfterMessages: afterMessages, SourceID: activity.ID}:
						default:
						}
					}
					if request.Approved {
						return runtime.loop.ApproveObserved(operationCtx, handle, actor, runtime.provider, approval, observer)
					}
					return runtime.loop.DenyObserved(operationCtx, handle, actor, runtime.provider, approval, "user_denied", observer)
				})
				continue
			}
			if err := sendUIState(sendState, config, messages, activities, pending, "READY"); err != nil {
				return err
			}
		}
	}
}

func listUIModels(ctx context.Context, runtime *runtime, handle *session.Handle, config config) ([]string, error) {
	if err := runtime.sessions.Record(ctx, handle, events.ModelListRequested, actor, events.ModelListRequestedPayload{Provider: config.provider}); err != nil {
		return nil, err
	}
	available, err := models.List(ctx, models.Config{Provider: config.provider, APIKey: config.apiKey})
	if err != nil {
		_ = runtime.sessions.Record(context.WithoutCancel(ctx), handle, events.ModelListFailed, actor, events.ModelListFailedPayload{Provider: config.provider, Code: "catalog_failed"})
		return nil, err
	}
	if err := runtime.sessions.Record(ctx, handle, events.ModelListCompleted, actor, events.ModelListCompletedPayload{Provider: config.provider, Count: len(available)}); err != nil {
		return nil, err
	}
	return available, nil
}

func activeTheme() string {
	settings, err := appconfig.Load()
	if err != nil || settings.Theme == "" {
		return "default"
	}
	return settings.Theme
}

func handleUICommand(ctx context.Context, runtime *runtime, handle *session.Handle, config *config, input string, allowAll bool) (string, bool, bool) {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return "", allowAll, false
	}
	switch parts[0] {
	case "/help":
		return "Commands: /model [NAME], /theme [default|contrast|mono], /allow-all [on|off], /settings", allowAll, true
	case "/settings":
		settings, err := appconfig.Load()
		if err != nil {
			return "Error: " + err.Error(), allowAll, true
		}
		theme := settings.Theme
		if theme == "" {
			theme = "default"
		}
		return fmt.Sprintf("Provider: %s  Model: %s  Theme: %s (next session)  Approval: %s", config.provider, config.model, theme, approvalModeLabel(allowAll)), allowAll, true
	case "/allow-all":
		enabled := len(parts) < 2 || parts[1] == "on"
		if len(parts) > 1 && parts[1] != "on" && parts[1] != "off" {
			return "Usage: /allow-all [on|off]", allowAll, true
		}
		if err := runtime.sessions.Record(ctx, handle, events.ApprovalModeChanged, actor, events.ApprovalModeChangedPayload{AllowAll: enabled}); err != nil {
			return "Error: " + err.Error(), allowAll, true
		}
		return "Approval mode: " + approvalModeLabel(enabled), enabled, true
	case "/theme":
		if len(parts) != 2 || (parts[1] != "default" && parts[1] != "contrast" && parts[1] != "mono") {
			return "Usage: /theme [default|contrast|mono]", allowAll, true
		}
		if err := runtime.sessions.Record(ctx, handle, events.SessionConfigChanged, actor, events.SessionConfigChangedPayload{Setting: "theme", Current: parts[1]}); err != nil {
			return "Error: " + err.Error(), allowAll, true
		}
		if err := appconfig.SaveTheme(parts[1]); err != nil {
			return "Error: " + err.Error(), allowAll, true
		}
		return "Theme: " + parts[1] + " (applies next session)", allowAll, true
	case "/model":
		if len(parts) == 1 {
			if err := runtime.sessions.Record(ctx, handle, events.ModelListRequested, actor, events.ModelListRequestedPayload{Provider: config.provider}); err != nil {
				return "Error: " + err.Error(), allowAll, true
			}
			available, err := models.List(ctx, models.Config{Provider: config.provider, APIKey: config.apiKey})
			if err != nil {
				_ = runtime.sessions.Record(context.WithoutCancel(ctx), handle, events.ModelListFailed, actor, events.ModelListFailedPayload{Provider: config.provider, Code: "catalog_failed"})
				return "Error: " + err.Error(), allowAll, true
			}
			if err := runtime.sessions.Record(ctx, handle, events.ModelListCompleted, actor, events.ModelListCompletedPayload{Provider: config.provider, Count: len(available)}); err != nil {
				return "Error: " + err.Error(), allowAll, true
			}
			return "Available models: " + strings.Join(available, ", "), allowAll, true
		}
		model := parts[1]
		if err := runtime.sessions.Record(ctx, handle, events.SessionConfigChanged, actor, events.SessionConfigChangedPayload{Setting: "model", Previous: config.model, Current: model}); err != nil {
			return "Error: " + err.Error(), allowAll, true
		}
		if err := appconfig.SaveConnection(config.provider, config.apiKey, model); err != nil {
			return "Error: " + err.Error(), allowAll, true
		}
		config.model = model
		return "Model: " + model, allowAll, true
	default:
		return "Unknown command. Run /help.", allowAll, true
	}
}

func approvalModeLabel(allowAll bool) string {
	if allowAll {
		return "allow all (session only)"
	}
	return "confirm each action"
}

func currentWorkspace() (string, error) {
	workspace, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	workspace, err = filepath.Abs(workspace)
	if err != nil {
		return "", fmt.Errorf("resolve workspace path: %w", err)
	}
	return workspace, nil
}

func sendUIState(send func(ui.State) error, config config, messages []agent.Message, activities []uiToolActivity, pending *agent.PendingApproval, status string) error {
	transcript := make([]ui.TranscriptEntry, 0, len(messages)+len(activities))
	for index, message := range messages {
		label, role := "You", "user"
		if message.Role == agent.RoleAssistant {
			label, role = config.model, "assistant"
		}
		if message.Content != "" {
			transcript = append(transcript, ui.TranscriptEntry{Role: role, Label: label, Content: message.Content})
		}
		transcript = appendToolActivities(transcript, activities, index+1, config.model)
	}
	transcript = appendToolActivities(transcript, activities, len(messages)+1, config.model)
	state := ui.State{Phase: "chat", Provider: config.provider, Model: config.model, Theme: activeTheme(), Workspace: config.workspace, Status: status, Transcript: transcript}
	if pending != nil {
		state.Approval = &ui.Approval{Action: pending.Action, Summary: pending.Summary, Hash: pending.Hash}
	}
	return send(state)
}

func upsertUIActivity(activities []uiToolActivity, update uiToolActivity) []uiToolActivity {
	if update.SourceID == "" {
		update.SourceID = update.Activity.ID
	}
	for index := len(activities) - 1; index >= 0; index-- {
		if activities[index].SourceID == update.SourceID && !terminalActivity(activities[index].Activity.Phase) {
			update.Activity.ID = activities[index].Activity.ID
			activities[index].Activity = update.Activity
			return activities
		}
	}
	update.Activity.ID = fmt.Sprintf("%s:%d", update.SourceID, len(activities))
	return append(activities, update)
}

func terminalActivity(phase agent.ActivityPhase) bool {
	return phase == agent.ActivityCompleted || phase == agent.ActivityFailed || phase == agent.ActivityDenied
}

func alignUIActivities(messages []agent.Message, activities []uiToolActivity) []uiToolActivity {
	occurrences := make(map[string]int)
	for messageIndex, message := range messages {
		for _, call := range message.ToolCalls {
			wanted := occurrences[call.ID]
			current := 0
			for activityIndex := range activities {
				if activities[activityIndex].SourceID == call.ID {
					if current == wanted {
						activities[activityIndex].AfterMessages = messageIndex + 1
						break
					}
					current++
				}
			}
			occurrences[call.ID]++
		}
	}
	return activities
}

func appendToolActivities(transcript []ui.TranscriptEntry, activities []uiToolActivity, afterMessages int, label string) []ui.TranscriptEntry {
	for _, entry := range activities {
		if entry.AfterMessages != afterMessages {
			continue
		}
		activity := entry.Activity
		transcript = append(transcript, ui.TranscriptEntry{Role: "activity", Label: label, Tool: &ui.ToolActivity{
			ID:               activity.ID,
			Name:             activity.Name,
			Phase:            string(activity.Phase),
			Target:           activity.Target,
			Command:          activity.Command,
			WorkingDirectory: activity.WorkingDirectory,
			Bytes:            activity.Bytes,
			Truncated:        activity.Truncated,
			ExitCode:         activity.ExitCode,
			OutputHidden:     activity.OutputHidden,
		}})
	}
	return transcript
}

func displayUIError(err error) string { return "Error: " + err.Error() }

func savedSetup(settings appconfig.Settings, workspace string) (tui.SetupConfig, bool) {
	provider := settings.Provider
	model := strings.TrimSpace(settings.Model)
	apiKey := strings.TrimSpace(providerAPIKey(provider, settings))
	if provider == "" || model == "" || apiKey == "" {
		return tui.SetupConfig{}, false
	}
	return tui.SetupConfig{Provider: provider, Model: model, Workspace: workspace, APIKey: apiKey}, true
}

type tuiRunner struct {
	runtime *runtime
	handle  *session.Handle
	config  config
}

func (r *tuiRunner) Turn(ctx context.Context, messages []agent.Message) (agent.LoopResult, error) {
	return r.runtime.loop.RunWithApproval(ctx, r.handle, actor, r.runtime.provider, agent.CompletionRequest{
		Model:    r.config.model,
		Messages: messages,
		Tools:    r.runtime.tools,
	})
}

func (r *tuiRunner) Resolve(ctx context.Context, pending *agent.PendingApproval, approved bool) (agent.LoopResult, error) {
	if approved {
		return r.runtime.loop.Approve(ctx, r.handle, actor, r.runtime.provider, pending)
	}
	return r.runtime.loop.Deny(ctx, r.handle, actor, r.runtime.provider, pending, "user_denied")
}

func (r *tuiRunner) ListModels(ctx context.Context) ([]string, error) {
	return r.listModels(ctx, r.config.provider, r.config.apiKey)
}

func (r *tuiRunner) ListModelsFor(ctx context.Context, provider, apiKey string) ([]string, error) {
	return r.listModels(ctx, provider, apiKey)
}

func (r *tuiRunner) listModels(ctx context.Context, provider, apiKey string) ([]string, error) {
	if err := r.runtime.sessions.Record(ctx, r.handle, events.ModelListRequested, actor, events.ModelListRequestedPayload{Provider: provider}); err != nil {
		return nil, fmt.Errorf("record model list request: %w", err)
	}
	listed, err := models.List(ctx, models.Config{Provider: provider, APIKey: apiKey})
	if err != nil {
		_ = r.runtime.sessions.Record(context.WithoutCancel(ctx), r.handle, events.ModelListFailed, actor, events.ModelListFailedPayload{Provider: provider, Code: "catalog_failed"})
		return nil, err
	}
	if err := r.runtime.sessions.Record(ctx, r.handle, events.ModelListCompleted, actor, events.ModelListCompletedPayload{Provider: provider, Count: len(listed)}); err != nil {
		return nil, fmt.Errorf("record model list completion: %w", err)
	}
	return listed, nil
}

func (r *tuiRunner) SetConnection(ctx context.Context, selected tui.SetupConfig) error {
	updated, err := configFromTUI(selected, appconfig.Settings{Transport: r.config.transport})
	if err != nil {
		return err
	}
	provider, err := newProvider(updated.provider, updated.transport, updated.apiKey)
	if err != nil {
		return err
	}
	if err := r.runtime.sessions.Record(ctx, r.handle, events.SessionConfigChanged, actor, events.SessionConfigChangedPayload{
		Setting: "connection", Previous: r.config.provider + " / " + r.config.model, Current: updated.provider + " / " + updated.model,
	}); err != nil {
		return fmt.Errorf("record connection change: %w", err)
	}
	if err := appconfig.SaveConnection(updated.provider, updated.apiKey, updated.model); err != nil {
		return fmt.Errorf("save connection: %w", err)
	}
	r.runtime.provider = provider
	r.config = updated
	return nil
}

func (r *tuiRunner) SetModel(ctx context.Context, model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return errors.New("model is required")
	}
	if model == r.config.model {
		return nil
	}
	if err := r.runtime.sessions.Record(ctx, r.handle, events.SessionConfigChanged, actor, events.SessionConfigChangedPayload{
		Setting: "model", Previous: r.config.model, Current: model,
	}); err != nil {
		return fmt.Errorf("record model change: %w", err)
	}
	if err := appconfig.SaveConnection(r.config.provider, r.config.apiKey, model); err != nil {
		return fmt.Errorf("save model selection: %w", err)
	}
	r.config.model = model
	return nil
}

func (r *tuiRunner) SetTheme(ctx context.Context, theme string) error {
	if err := r.runtime.sessions.Record(ctx, r.handle, events.SessionConfigChanged, actor, events.SessionConfigChangedPayload{
		Setting: "theme", Current: theme,
	}); err != nil {
		return fmt.Errorf("record theme change: %w", err)
	}
	if err := appconfig.SaveTheme(theme); err != nil {
		return fmt.Errorf("save theme selection: %w", err)
	}
	if err := tui.SetTheme(theme); err != nil {
		return err
	}
	return nil
}

func (r *tuiRunner) SetAllowAll(ctx context.Context, enabled bool) error {
	if err := r.runtime.sessions.Record(ctx, r.handle, events.ApprovalModeChanged, actor, events.ApprovalModeChangedPayload{AllowAll: enabled}); err != nil {
		return fmt.Errorf("record approval mode change: %w", err)
	}
	return nil
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
	defer func() { _ = runtime.close() }()

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
	settings, err := appconfig.Load()
	if err != nil {
		return err
	}
	connectionString := settings.KurrentDBURL
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
	settings, err := appconfig.Load()
	if err != nil {
		return config{}, err
	}
	return parseConfigWithSettings(args, settings)
}

func parseConfigWithSettings(args []string, settings appconfig.Settings) (config, error) {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	provider := flags.String("provider", settings.Provider, "provider: openai, anthropic, or opencode")
	transport := flags.String("transport", settings.Transport, "OpenCode transport: responses or chat-completions")
	model := flags.String("model", settings.Model, "model name")
	workspaceRoot := flags.String("workspace", settings.Workspace, "workspace root")
	if len(args) == 0 || args[0] != "run" {
		return config{}, errors.New("usage: symphony run --provider PROVIDER --model MODEL [--workspace PATH] PROMPT")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return config{}, err
	}
	if flags.NArg() != 1 || strings.TrimSpace(flags.Arg(0)) == "" {
		return config{}, errors.New("a prompt is required")
	}
	if err := validateProviderTransport(*provider, *transport); err != nil {
		return config{}, err
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
	return config{
		provider:         *provider,
		transport:        *transport,
		model:            *model,
		workspace:        root,
		prompt:           flags.Arg(0),
		connectionString: settings.KurrentDBURL,
		apiKey:           providerAPIKey(*provider, settings),
	}, nil
}

func configFromTUI(selected tui.SetupConfig, settings appconfig.Settings) (config, error) {
	transport := settings.Transport
	if transport == "" {
		transport = opencode.TransportResponses
	}
	if selected.Provider == "opencode-go" {
		transport = openCodeGoTransport(selected.Model)
	}
	if err := validateProviderTransport(selected.Provider, transport); err != nil {
		return config{}, err
	}
	if strings.TrimSpace(selected.Model) == "" {
		return config{}, errors.New("model is required")
	}
	return config{
		provider:         selected.Provider,
		transport:        transport,
		model:            selected.Model,
		workspace:        selected.Workspace,
		connectionString: localKurrentDBURL,
		apiKey:           strings.TrimSpace(selected.APIKey),
	}, nil
}

func validateProviderTransport(provider, transport string) error {
	switch provider {
	case "openai", "anthropic":
		return nil
	case "opencode":
		if transport == opencode.TransportResponses || transport == opencode.TransportChat {
			return nil
		}
	case "opencode-go":
		if transport == opencode.TransportResponses || transport == opencode.TransportChat || transport == "messages" {
			return nil
		}
	default:
		return errors.New("provider must be openai, anthropic, opencode, or opencode-go")
	}
	return errors.New("OpenCode transport must be responses or chat-completions")
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
	if config.connectionString == "" {
		return nil, errors.New("KURRENTDB_URL is required")
	}
	store, err := kurrentdb.New(config.connectionString)
	if err != nil {
		return nil, err
	}
	provider, err := newProvider(config.provider, config.transport, config.apiKey)
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

func providerAPIKey(provider string, settings appconfig.Settings) string {
	switch provider {
	case "openai":
		return settings.OpenAIAPIKey
	case "anthropic":
		return settings.AnthropicAPIKey
	case "opencode", "opencode-go":
		return settings.OpenCodeAPIKey
	default:
		return ""
	}
}

func newProvider(name, transport, apiKey string) (agent.Provider, error) {
	apiKey = strings.TrimSpace(apiKey)
	switch name {
	case "openai":
		return openai.New(openai.Config{APIKey: apiKey})
	case "anthropic":
		return anthropic.New(anthropic.Config{APIKey: apiKey})
	case "opencode":
		return opencode.New(opencode.Config{APIKey: apiKey, Transport: transport})
	case "opencode-go":
		if transport == "messages" {
			return anthropic.New(anthropic.Config{APIKey: apiKey, BaseURL: "https://opencode.ai/zen/go", ProviderName: "OpenCode Go", BearerAuth: true})
		}
		return opencode.New(opencode.Config{APIKey: apiKey, BaseURL: "https://opencode.ai/zen/go/v1", Transport: transport})
	default:
		return nil, errors.New("provider must be openai, anthropic, or opencode")
	}
}

func openCodeGoTransport(model string) string {
	switch model {
	case "gpt-5.6-luna":
		return opencode.TransportResponses
	case "minimax-m3", "minimax-m2.7", "minimax-m2.5", "qwen3.8-max", "qwen3.7-max", "qwen3.7-plus", "qwen3.6-plus", "qwen3.5-plus":
		return "messages"
	default:
		return opencode.TransportChat
	}
}
