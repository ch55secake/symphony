// Package tui provides Symphony's interactive terminal interface.
package tui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ch55secake/symphony/internal/agent"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var ErrCanceled = errors.New("TUI canceled")

// Config identifies the session displayed by the TUI.
type Config struct {
	Provider      string
	Model         string
	Workspace     string
	SessionID     string
	InitialPrompt string
}

// Runner executes agent turns and resolves side-effect approvals.
type Runner interface {
	Turn(context.Context, []agent.Message) (agent.LoopResult, error)
	Resolve(context.Context, *agent.PendingApproval, bool) (agent.LoopResult, error)
}

// Run starts the full-screen interactive interface.
func Run(ctx context.Context, config Config, runner Runner) error {
	if runner == nil {
		return errors.New("TUI runner is required")
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	program := tea.NewProgram(newModel(runCtx, cancel, config, runner), tea.WithAltScreen(), tea.WithContext(runCtx))
	result, err := program.Run()
	if err != nil {
		if runCtx.Err() != nil {
			return ErrCanceled
		}
		return err
	}
	final, ok := result.(model)
	if !ok {
		return errors.New("unexpected TUI model")
	}
	if final.canceled || ctx.Err() != nil {
		return ErrCanceled
	}
	return nil
}

type model struct {
	ctx      context.Context
	cancel   context.CancelFunc
	config   Config
	runner   Runner
	input    textinput.Model
	viewport viewport.Model
	messages []agent.Message
	pending  *agent.PendingApproval
	busy     bool
	canceled bool
	err      error
	width    int
	height   int
}

type turnResultMsg struct {
	result agent.LoopResult
	err    error
}

type initialSubmitMsg struct{}

func newModel(ctx context.Context, cancel context.CancelFunc, config Config, runner Runner) model {
	input := textinput.New()
	input.Placeholder = "Describe what you need..."
	input.Prompt = ""
	input.TextStyle = commandTextStyle
	input.PlaceholderStyle = subtleStyle
	input.SetValue(config.InitialPrompt)
	input.CharLimit = 32 << 10
	input.Focus()
	return model{
		ctx:      ctx,
		cancel:   cancel,
		config:   config,
		runner:   runner,
		input:    input,
		viewport: viewport.New(80, 12),
	}
}

func (m model) Init() tea.Cmd {
	if strings.TrimSpace(m.config.InitialPrompt) != "" {
		return func() tea.Msg { return initialSubmitMsg{} }
	}
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.Width = max(20, msg.Width-4)
		m.viewport.Width = max(20, msg.Width-4)
		m.viewport.Height = max(3, msg.Height-10)
		m.refreshConversation()
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.canceled = true
			m.cancel()
			return m, tea.Quit
		}
		if m.pending != nil {
			switch msg.String() {
			case "y", "Y":
				return m.resolve(true)
			case "n", "N", "esc":
				return m.resolve(false)
			}
			return m, nil
		}
		if !m.busy && (msg.String() == "ctrl+q" || msg.String() == "esc") && strings.TrimSpace(m.input.Value()) == "" {
			return m, tea.Quit
		}
		if !m.busy && msg.String() == "enter" && strings.TrimSpace(m.input.Value()) != "" {
			return m.submit()
		}
		if msg.String() == "pgup" || msg.String() == "pgdown" || msg.String() == "ctrl+up" || msg.String() == "ctrl+down" {
			var command tea.Cmd
			m.viewport, command = m.viewport.Update(msg)
			return m, command
		}
	case turnResultMsg:
		m.busy = false
		if msg.err != nil {
			m.err = displayError(msg.err)
			return m, nil
		}
		m.err = nil
		m.messages = append([]agent.Message(nil), msg.result.Messages...)
		m.pending = msg.result.Pending
		m.refreshConversation()
		return m, nil
	case initialSubmitMsg:
		return m.submit()
	}

	if m.busy {
		return m, nil
	}
	var command tea.Cmd
	m.input, command = m.input.Update(msg)
	return m, command
}

func displayError(err error) error {
	if strings.Contains(err.Error(), "HTTP 401") {
		return fmt.Errorf("%w; restart Symphony and run /connect", err)
	}
	return err
}

func (m model) submit() (tea.Model, tea.Cmd) {
	prompt := m.input.Value()
	m.messages = append(m.messages, agent.Message{Role: agent.RoleUser, Content: prompt})
	m.input.Reset()
	m.busy = true
	m.err = nil
	m.refreshConversation()
	messages := append([]agent.Message(nil), m.messages...)
	return m, func() tea.Msg {
		result, err := m.runner.Turn(m.ctx, messages)
		return turnResultMsg{result: result, err: err}
	}
}

func (m model) resolve(approved bool) (tea.Model, tea.Cmd) {
	pending := m.pending
	m.pending = nil
	m.busy = true
	m.err = nil
	return m, func() tea.Msg {
		result, err := m.runner.Resolve(m.ctx, pending, approved)
		return turnResultMsg{result: result, err: err}
	}
}

func (m *model) refreshConversation() {
	lines := make([]string, 0, len(m.messages)*2)
	for _, message := range m.messages {
		switch message.Role {
		case agent.RoleUser:
			lines = append(lines, userStyle.Render("You"), message.Content)
		case agent.RoleAssistant:
			if message.Content != "" {
				lines = append(lines, assistantStyle.Render(m.config.Model), message.Content)
			}
			for _, call := range message.ToolCalls {
				lines = append(lines, activityStyle.Render(fmt.Sprintf("Requested %s", call.Name)))
			}
		}
	}
	m.viewport.SetContent(strings.Join(lines, "\n\n"))
	m.viewport.GotoBottom()
}

func (m model) View() string {
	if m.width == 0 {
		return "Loading Symphony..."
	}
	header := titleStyle.Render("SYMPHONY") + "  " + subtleStyle.Render(fmt.Sprintf("%s / %s", m.config.Provider, m.config.Model)) + "  " + subtleStyle.Render(filepath.Base(m.config.Workspace))
	status := subtleStyle.Render("READY")
	if m.busy {
		status = activityStyle.Render("WORKING")
	}
	if m.err != nil {
		status = errorStyle.Render("Error: " + m.err.Error())
	}
	if m.pending != nil {
		status = approvalStyle.Render(fmt.Sprintf("Approval required: %s\nHash: %s\n[y] approve  [n/Esc] deny", m.pending.Summary, m.pending.Hash))
	}
	composer := composerStyle.Width(max(20, m.width-5)).Render(commandPromptStyle.Render("ASK  ") + m.input.View())
	return header + "\n\n" + m.viewport.View() + "\n" + status + "\n" + composer
}

var (
	titleStyle         = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("69"))
	subtleStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	userStyle          = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75"))
	assistantStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	activityStyle      = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("244"))
	approvalStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	errorStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	commandPromptStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("69"))
	commandTextStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	composerStyle      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("69")).Padding(0, 1)
)
