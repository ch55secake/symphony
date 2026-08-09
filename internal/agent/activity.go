package agent

import (
	"encoding/json"
	"strconv"
	"strings"
	"unicode"

	"github.com/ch55secake/symphony/internal/audit"
	"github.com/ch55secake/symphony/internal/events"
)

// ActivityPhase identifies the visible lifecycle of a model-requested tool call.
type ActivityPhase string

const (
	// ActivityRequested indicates that the model requested a tool invocation.
	ActivityRequested ActivityPhase = "requested"
	// ActivityRunning indicates that a tool is executing.
	ActivityRunning ActivityPhase = "running"
	// ActivityAwaitingApproval indicates that an operator decision is required.
	ActivityAwaitingApproval ActivityPhase = "awaiting_approval"
	// ActivityCompleted indicates successful tool execution.
	ActivityCompleted ActivityPhase = "completed"
	// ActivityFailed indicates that tool execution failed.
	ActivityFailed ActivityPhase = "failed"
	// ActivityDenied indicates that an operator denied the tool action.
	ActivityDenied ActivityPhase = "denied"
)

// ToolActivity is an allowlisted, display-safe projection of a tool call.
type ToolActivity struct {
	ID               string
	Name             string
	Phase            ActivityPhase
	Target           string
	Command          string
	WorkingDirectory string
	Bytes            int
	Truncated        bool
	ExitCode         *int
	OutputHidden     bool
}

// ActivityObserver receives tool lifecycle updates in execution order.
type ActivityObserver func(ToolActivity)

// ProjectToolActivity removes raw arguments and retains only safe display metadata.
func ProjectToolActivity(call events.ModelToolCall, phase ActivityPhase) ToolActivity {
	activity := ToolActivity{ID: call.ID, Name: sanitizeActivityText(call.Name, 48), Phase: phase}
	switch call.Name {
	case "read_file":
		var input struct {
			Path string `json:"path"`
		}
		if json.Unmarshal(call.Arguments, &input) == nil {
			activity.Target = sanitizeActivityText(input.Path, 160)
		}
	case "write_file":
		var input struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if json.Unmarshal(call.Arguments, &input) == nil {
			activity.Target = sanitizeActivityText(input.Path, 160)
			activity.Bytes = len([]byte(input.Content))
			activity.OutputHidden = true
		}
	case "run_command":
		var input struct {
			Executable       string   `json:"executable"`
			Arguments        []string `json:"arguments"`
			WorkingDirectory string   `json:"working_directory"`
		}
		if json.Unmarshal(call.Arguments, &input) == nil {
			activity.Command = commandPreview(input.Executable, input.Arguments)
			activity.WorkingDirectory = sanitizeActivityText(input.WorkingDirectory, 120)
			activity.OutputHidden = true
		}
	}
	return activity
}

func emitActivity(observer ActivityObserver, activity ToolActivity) {
	if observer != nil {
		observer(activity)
	}
}

func completeActivity(activity ToolActivity, result ToolResult, phase ActivityPhase) ToolActivity {
	activity.Phase = phase
	activity.Bytes = result.Bytes
	activity.Truncated = result.Truncated
	activity.ExitCode = result.ExitCode
	return activity
}

func commandPreview(executable string, arguments []string) string {
	safeArguments := append([]string(nil), arguments...)
	if raw, _, err := audit.DefaultPolicy().Redact(safeArguments); err == nil {
		_ = json.Unmarshal(raw, &safeArguments)
	}
	parts := []string{sanitizeActivityText(executable, 80)}
	for _, argument := range safeArguments {
		argument = sanitizeActivityText(argument, 100)
		if strings.ContainsAny(argument, " \t") {
			argument = strconv.Quote(argument)
		}
		parts = append(parts, argument)
	}
	return sanitizeActivityText(strings.Join(parts, " "), 200)
}

func sanitizeActivityText(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u202a' || r == '\u202b' || r == '\u202d' || r == '\u202e' || r == '\u2066' || r == '\u2067' || r == '\u2068' || r == '\u2069' {
			return -1
		}
		return r
	}, value)
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit-3]) + "..."
	}
	return value
}
