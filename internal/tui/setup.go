package tui

import (
	"context"
	"errors"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var providers = []string{"opencode", "openai", "anthropic"}

// SetupConfig contains the session settings chosen before the agent starts.
type SetupConfig struct {
	Provider  string
	Model     string
	Workspace string
}

// Select displays the startup settings before creating a session.
func Select(ctx context.Context, config SetupConfig) (SetupConfig, error) {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	program := tea.NewProgram(newSetupModel(runCtx, cancel, config), tea.WithAltScreen(), tea.WithContext(runCtx))
	result, err := program.Run()
	if err != nil {
		if runCtx.Err() != nil {
			return SetupConfig{}, ErrCanceled
		}
		return SetupConfig{}, err
	}
	final, ok := result.(setupModel)
	if !ok {
		return SetupConfig{}, errors.New("unexpected TUI setup model")
	}
	if final.canceled || ctx.Err() != nil {
		return SetupConfig{}, ErrCanceled
	}
	return SetupConfig{Provider: final.provider(), Model: strings.TrimSpace(final.model.Value()), Workspace: final.workspace}, nil
}

type setupModel struct {
	ctx       context.Context
	cancel    context.CancelFunc
	providerI int
	model     textinput.Model
	workspace string
	canceled  bool
	err       string
	width     int
}

func newSetupModel(ctx context.Context, cancel context.CancelFunc, config SetupConfig) setupModel {
	input := textinput.New()
	input.Prompt = "Model: "
	input.Placeholder = "Enter a model name"
	input.SetValue(config.Model)
	input.CharLimit = 512
	return setupModel{
		ctx:       ctx,
		cancel:    cancel,
		providerI: providerIndex(config.Provider),
		model:     input,
		workspace: config.Workspace,
	}
}

func (m setupModel) Init() tea.Cmd {
	return m.model.Focus()
}

func (m setupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.model.Width = max(20, msg.Width-14)
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "ctrl+q", "esc":
			m.canceled = true
			m.cancel()
			return m, tea.Quit
		case "tab", "right", "down":
			m.providerI = (m.providerI + 1) % len(providers)
			return m, nil
		case "shift+tab", "left", "up":
			m.providerI = (m.providerI + len(providers) - 1) % len(providers)
			return m, nil
		case "enter":
			if strings.TrimSpace(m.model.Value()) == "" {
				m.err = "A model is required."
				return m, nil
			}
			return m, tea.Quit
		}
	}
	var command tea.Cmd
	m.model, command = m.model.Update(msg)
	return m, command
}

func (m setupModel) View() string {
	if m.width == 0 {
		return "Loading Symphony..."
	}
	provider := setupSelectedStyle.Render(m.provider())
	status := "Tab changes provider. Enter starts the session. Esc cancels."
	if m.err != "" {
		status = errorStyle.Render(m.err)
	}
	return titleStyle.Render("SYMPHONY") + "\n\n" +
		"Provider: " + provider + subtleStyle.Render("  [Tab]") + "\n" +
		m.model.View() + "\n\n" +
		subtleStyle.Render("Workspace: "+m.workspace) + "\n\n" + status
}

func (m setupModel) provider() string {
	return providers[m.providerI]
}

func providerIndex(provider string) int {
	for index, candidate := range providers {
		if candidate == provider {
			return index
		}
	}
	return 0
}

var setupSelectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
