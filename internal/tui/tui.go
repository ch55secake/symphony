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
	ListModels(context.Context) ([]string, error)
	SetModel(context.Context, string) error
	SetTheme(context.Context, string) error
	SetAllowAll(context.Context, bool) error
	ListModelsFor(context.Context, string, string) ([]string, error)
	SetConnection(context.Context, SetupConfig) error
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
	allowAll bool
	mode     commandMode
	models   []string
	selected int
	connect  *setupModel
	commands commandList
	width    int
	height   int
}

type turnResultMsg struct {
	result agent.LoopResult
	err    error
}

type initialSubmitMsg struct{}

type commandMode int

const (
	commandNormal commandMode = iota
	commandModels
	commandThemes
	commandSettings
	commandConfirmAllowAll
)

type chatModelListMsg struct {
	models []string
	err    error
}

type commandResultMsg struct {
	kind  string
	value string
	err   error
}

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
		commands: commandList{viewport: viewport.New(80, 4)},
	}
}

func (m model) Init() tea.Cmd {
	if strings.TrimSpace(m.config.InitialPrompt) != "" {
		return func() tea.Msg { return initialSubmitMsg{} }
	}
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.connect != nil {
		return m.updateConnect(msg)
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.Width = max(20, msg.Width-4)
		m.viewport.Width = max(20, msg.Width-4)
		m.viewport.Height = max(3, msg.Height-10)
		m.commands.viewport.Width = max(20, msg.Width-5)
		m.refreshCommandSuggestions()
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
		if m.mode != commandNormal {
			return m.updateCommandMode(msg)
		}
		if !m.busy && m.commands.active() {
			switch msg.String() {
			case "up":
				m.commands.up()
				return m, nil
			case "down":
				m.commands.down()
				return m, nil
			case "tab":
				m.input.SetValue(m.commands.selectedCommand())
				m.refreshCommandSuggestions()
				return m, nil
			}
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
		if m.pending != nil && m.allowAll {
			return m.resolve(true)
		}
		return m, nil
	case initialSubmitMsg:
		return m.submit()
	case chatModelListMsg:
		m.busy = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		if len(msg.models) == 0 {
			m.err = errors.New("no models are available for this provider")
			return m, nil
		}
		m.models = msg.models
		m.selected = modelIndex(m.models, m.config.Model)
		m.mode = commandModels
		return m, nil
	case commandResultMsg:
		m.busy = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		switch msg.kind {
		case "model":
			m.config.Model = msg.value
			m.refreshConversation()
		case "theme":
			if err := SetTheme(msg.value); err != nil {
				m.err = err
			}
		case "connection":
			parts := strings.SplitN(msg.value, " / ", 2)
			if len(parts) == 2 {
				m.config.Provider, m.config.Model = parts[0], parts[1]
				m.refreshConversation()
			}
		case "allow-all":
			m.allowAll = msg.value == "on"
		}
		return m, nil
	}

	if m.busy {
		return m, nil
	}
	var command tea.Cmd
	m.input, command = m.input.Update(msg)
	m.refreshCommandSuggestions()
	return m, command
}

func displayError(err error) error {
	if strings.Contains(err.Error(), "HTTP 401") {
		return fmt.Errorf("%w; restart Symphony and run /connect", err)
	}
	return err
}

func (m model) submit() (tea.Model, tea.Cmd) {
	prompt := strings.TrimSpace(m.input.Value())
	if strings.HasPrefix(prompt, "/") {
		m.input.Reset()
		return m.command(prompt)
	}
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

func (m model) command(input string) (tea.Model, tea.Cmd) {
	parts := strings.Fields(input)
	name := strings.ToLower(parts[0])
	argument := ""
	if len(parts) > 1 {
		argument = strings.ToLower(parts[1])
	}
	m.err = nil
	switch name {
	case "/model":
		if len(parts) > 1 {
			return m.applyModel(parts[1])
		}
		m.busy = true
		return m, func() tea.Msg {
			models, err := m.runner.ListModels(m.ctx)
			return chatModelListMsg{models: models, err: err}
		}
	case "/theme":
		if argument == "" {
			m.mode = commandThemes
			m.selected = themeIndex(currentTheme())
			return m, nil
		}
		return m.applyTheme(argument)
	case "/allow-all":
		if argument == "off" {
			return m.applyAllowAll(false)
		}
		if argument == "" || argument == "on" {
			m.mode = commandConfirmAllowAll
			return m, nil
		}
		m.err = errors.New("usage: /allow-all [on|off]")
	case "/settings":
		m.mode = commandSettings
	case "/connect":
		setup := newSetupModel(m.ctx, m.cancel, SetupConfig{Provider: m.config.Provider, Model: m.config.Model, Workspace: m.config.Workspace}, m.runner.ListModelsFor)
		setup.embedded = true
		m.connect = &setup
	case "/help":
		m.err = errors.New("commands: /connect, /model, /theme, /allow-all [on|off], /settings")
	default:
		m.err = fmt.Errorf("unknown command %q; run /help", name)
	}
	return m, nil
}

func (m model) updateCommandMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case commandModels:
		switch msg.String() {
		case "up", "k":
			m.selected = max(0, m.selected-1)
		case "down", "j":
			m.selected = min(len(m.models)-1, m.selected+1)
		case "enter":
			return m.applyModel(m.models[m.selected])
		case "esc":
			m.mode = commandNormal
		}
	case commandThemes:
		switch msg.String() {
		case "up", "k":
			m.selected = max(0, m.selected-1)
		case "down", "j":
			m.selected = min(len(themeNames())-1, m.selected+1)
		case "enter":
			return m.applyTheme(themeNames()[m.selected])
		case "esc":
			m.mode = commandNormal
		}
	case commandSettings:
		if msg.String() == "esc" || msg.String() == "enter" {
			m.mode = commandNormal
		}
	case commandConfirmAllowAll:
		switch msg.String() {
		case "y", "Y":
			return m.applyAllowAll(true)
		case "n", "N", "esc":
			m.mode = commandNormal
		}
	}
	return m, nil
}

func (m model) updateConnect(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, command := m.connect.Update(msg)
	setup := updated.(setupModel)
	m.connect = &setup
	if !setup.complete {
		return m, command
	}
	selected := SetupConfig{Provider: setup.provider(), Model: setup.models[setup.selected], Workspace: setup.workspace, APIKey: strings.TrimSpace(setup.apiKey.Value())}
	m.connect = nil
	m.busy = true
	return m, func() tea.Msg {
		return commandResultMsg{kind: "connection", value: selected.Provider + " / " + selected.Model, err: m.runner.SetConnection(m.ctx, selected)}
	}
}

func (m model) applyModel(name string) (tea.Model, tea.Cmd) {
	m.mode, m.busy = commandNormal, true
	return m, func() tea.Msg {
		return commandResultMsg{kind: "model", value: name, err: m.runner.SetModel(m.ctx, name)}
	}
}

func (m model) applyTheme(name string) (tea.Model, tea.Cmd) {
	if !validTheme(name) {
		m.err = fmt.Errorf("unknown theme %q", name)
		return m, nil
	}
	m.mode, m.busy = commandNormal, true
	return m, func() tea.Msg {
		return commandResultMsg{kind: "theme", value: name, err: m.runner.SetTheme(m.ctx, name)}
	}
}

func (m model) applyAllowAll(enabled bool) (tea.Model, tea.Cmd) {
	m.mode, m.busy = commandNormal, true
	return m, func() tea.Msg {
		value := "off"
		if enabled {
			value = "on"
		}
		return commandResultMsg{kind: "allow-all", value: value, err: m.runner.SetAllowAll(m.ctx, enabled)}
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
	if m.connect != nil {
		return m.connect.View()
	}
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
	if m.allowAll {
		status = approvalStyle.Render("ALLOW ALL (session only)")
	}
	composer := composerStyle.Width(max(20, m.width-5)).Render(commandPromptStyle.Render("ASK  ") + m.input.View())
	suggestions := m.suggestionView()
	return header + "\n\n" + m.viewport.View() + "\n" + status + "\n" + m.commandView() + suggestions + "\n" + composer
}

func (m model) suggestionView() string {
	if m.busy || m.mode != commandNormal || !m.commands.active() {
		return ""
	}
	content := m.commands.viewport.View() + "\n" + subtleStyle.Render("Up/Down selects  ·  Tab completes")
	return "\n" + suggestionStyle.Width(max(20, m.width-5)).Render(content)
}

func commandSuggestions(input string) []string {
	prefix := strings.ToLower(strings.TrimSpace(input))
	if !strings.HasPrefix(prefix, "/") || strings.Contains(prefix, " ") {
		return nil
	}
	commands := []string{"/allow-all", "/connect", "/help", "/model", "/settings", "/theme"}
	suggestions := make([]string, 0, len(commands))
	for _, command := range commands {
		if strings.HasPrefix(command, prefix) && command != prefix {
			suggestions = append(suggestions, command)
		}
	}
	return suggestions
}

type commandList struct {
	viewport viewport.Model
	items    []string
	selected int
}

func (l *commandList) active() bool {
	return len(l.items) > 0
}

func (l *commandList) up() {
	l.selected = max(0, l.selected-1)
	l.refresh()
}

func (l *commandList) down() {
	l.selected = min(len(l.items)-1, l.selected+1)
	l.refresh()
}

func (l *commandList) selectedCommand() string {
	return l.items[l.selected]
}

func (l *commandList) refresh() {
	lines := make([]string, len(l.items))
	for index, command := range l.items {
		if index == l.selected {
			lines[index] = setupSelectedStyle.Render("> " + command)
		} else {
			lines[index] = subtleStyle.Render("  " + command)
		}
	}
	l.viewport.Height = min(4, len(l.items))
	l.viewport.SetContent(strings.Join(lines, "\n"))
	l.viewport.SetYOffset(max(0, l.selected-l.viewport.Height+1))
}

func (m *model) refreshCommandSuggestions() {
	items := commandSuggestions(m.input.Value())
	if len(items) == 0 {
		m.commands.items = nil
		m.commands.selected = 0
		m.commands.viewport.SetContent("")
		return
	}
	m.commands.items = items
	m.commands.selected = min(m.commands.selected, len(items)-1)
	m.commands.refresh()
}

func (m model) commandView() string {
	switch m.mode {
	case commandModels:
		return "SELECT MODEL\n" + selectedList(m.models, m.selected) + "\n" + subtleStyle.Render("Up/Down chooses. Enter applies. Esc cancels.")
	case commandThemes:
		return "SELECT THEME\n" + selectedList(themeNames(), m.selected) + "\n" + subtleStyle.Render("Up/Down chooses. Enter applies. Esc cancels.")
	case commandSettings:
		return "SETTINGS\n" + subtleStyle.Render("Provider: "+m.config.Provider+"  Model: "+m.config.Model+"  Theme: "+currentTheme()+"  Approval: "+approvalMode(m.allowAll)+"\n/connect changes provider, API key, and model  /theme changes theme  /allow-all changes approval mode\nEnter or Esc closes")
	case commandConfirmAllowAll:
		return approvalStyle.Render("Allow all workspace writes and commands for this session? [y] enable  [n/Esc] cancel")
	default:
		return ""
	}
}

func selectedList(items []string, selected int) string {
	lines := make([]string, len(items))
	for i, item := range items {
		if i == selected {
			lines[i] = setupSelectedStyle.Render("> " + item)
		} else {
			lines[i] = subtleStyle.Render("  " + item)
		}
	}
	return strings.Join(lines, "\n")
}

func approvalMode(allowAll bool) string {
	if allowAll {
		return "allow all"
	}
	return "confirm each action"
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
	suggestionStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("244")).Padding(0, 1)
)
