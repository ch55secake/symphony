package tui

import (
	"context"
	"errors"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
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
	APIKey    string
}

// ModelLister returns the models accessible with the given provider credential.
type ModelLister func(context.Context, string, string) ([]string, error)

// Select displays the centered startup command flow before creating a session.
func Select(ctx context.Context, config SetupConfig, listModels ModelLister) (SetupConfig, error) {
	if listModels == nil {
		return SetupConfig{}, errors.New("model lister is required")
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	program := tea.NewProgram(newSetupModel(runCtx, cancel, config, listModels), tea.WithAltScreen(), tea.WithContext(runCtx))
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
	if final.stage != setupModels || len(final.models) == 0 {
		return SetupConfig{}, errors.New("provider connection was not completed")
	}
	return SetupConfig{Provider: final.provider(), Model: final.models[final.selected], Workspace: final.workspace, APIKey: strings.TrimSpace(final.apiKey.Value())}, nil
}

type setupStage int

const (
	setupCommand setupStage = iota
	setupConnect
	setupModels
)

type modelListMsg struct {
	models []string
	err    error
}

type setupModel struct {
	ctx        context.Context
	cancel     context.CancelFunc
	listModels ModelLister
	stage      setupStage
	providerI  int
	command    textinput.Model
	apiKey     textinput.Model
	models     []string
	model      string
	selected   int
	workspace  string
	spinner    spinner.Model
	loading    bool
	canceled   bool
	err        string
	width      int
	height     int
}

func newSetupModel(ctx context.Context, cancel context.CancelFunc, config SetupConfig, listModels ModelLister) setupModel {
	command := textinput.New()
	command.Prompt = "> "
	command.Placeholder = "/connect"
	command.Focus()
	apiKey := textinput.New()
	apiKey.Prompt = "API key: "
	apiKey.Placeholder = "Paste provider API key"
	apiKey.SetValue(config.APIKey)
	apiKey.EchoMode = textinput.EchoPassword
	apiKey.EchoCharacter = '*'
	indicator := spinner.New(spinner.WithSpinner(spinner.Dot))
	indicator.Style = titleStyle
	return setupModel{
		ctx:        ctx,
		cancel:     cancel,
		listModels: listModels,
		providerI:  providerIndex(config.Provider),
		command:    command,
		apiKey:     apiKey,
		model:      config.Model,
		workspace:  config.Workspace,
		spinner:    indicator,
	}
}

func (m setupModel) Init() tea.Cmd {
	return nil
}

func (m setupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.command.Width = max(20, msg.Width-8)
		m.apiKey.Width = max(20, msg.Width-14)
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "ctrl+q" || msg.String() == "esc" {
			m.canceled = true
			m.cancel()
			return m, tea.Quit
		}
		if m.loading {
			return m, nil
		}
		switch m.stage {
		case setupCommand:
			if msg.String() == "enter" {
				if strings.TrimSpace(m.command.Value()) != "/connect" {
					m.err = "Type /connect to choose a provider."
					return m, nil
				}
				m.stage = setupConnect
				m.err = ""
				return m, m.apiKey.Focus()
			}
		case setupConnect:
			switch msg.String() {
			case "tab", "right", "down":
				m.providerI = (m.providerI + 1) % len(providers)
				return m, nil
			case "shift+tab", "left", "up":
				m.providerI = (m.providerI + len(providers) - 1) % len(providers)
				return m, nil
			case "enter":
				if strings.TrimSpace(m.apiKey.Value()) == "" {
					m.err = "An API key is required to connect."
					return m, nil
				}
				m.loading = true
				m.err = ""
				provider, apiKey := m.provider(), strings.TrimSpace(m.apiKey.Value())
				return m, tea.Batch(m.spinner.Tick, func() tea.Msg {
					models, err := m.listModels(m.ctx, provider, apiKey)
					return modelListMsg{models: models, err: err}
				})
			}
		case setupModels:
			switch msg.String() {
			case "up", "k":
				m.selected = max(0, m.selected-1)
				return m, nil
			case "down", "j":
				m.selected = min(len(m.models)-1, m.selected+1)
				return m, nil
			case "enter":
				return m, tea.Quit
			}
		}
	case modelListMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.models = msg.models
		m.selected = modelIndex(m.models, m.model)
		m.stage = setupModels
		return m, nil
	case spinner.TickMsg:
		if m.loading {
			var command tea.Cmd
			m.spinner, command = m.spinner.Update(msg)
			return m, command
		}
	}

	var command tea.Cmd
	if m.stage == setupCommand {
		m.command, command = m.command.Update(msg)
	} else if m.stage == setupConnect {
		m.apiKey, command = m.apiKey.Update(msg)
	}
	return m, command
}

func (m setupModel) View() string {
	if m.width == 0 {
		return "Loading Symphony..."
	}
	var content string
	switch m.stage {
	case setupCommand:
		content = titleStyle.Render("SYMPHONY") + "\n\n" + subtleStyle.Render("A local coding session, recorded as an event stream.") + "\n\n" + m.command.View() + "\n\n" + subtleStyle.Render("Type /connect to configure a provider.")
	case setupConnect:
		content = titleStyle.Render("CONNECT") + "\n\n" + "Provider: " + setupSelectedStyle.Render(m.provider()) + subtleStyle.Render("  [Tab]") + "\n" + m.apiKey.View() + "\n\n" + subtleStyle.Render("Enter fetches the models available to this key.")
	case setupModels:
		content = titleStyle.Render("SELECT MODEL") + "\n\n" + setupSelectedStyle.Render(m.provider()) + "\n\n" + m.modelView() + "\n\n" + subtleStyle.Render("Up/Down chooses. Enter saves and starts.")
	}
	if m.loading {
		content += "\n\n" + m.spinner.View() + " Fetching models..."
	}
	if m.err != "" {
		content += "\n\n" + errorStyle.Render(m.err)
	}
	content += "\n\n" + subtleStyle.Render("Workspace: "+m.workspace)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

func (m setupModel) modelView() string {
	start := max(0, m.selected-4)
	end := min(len(m.models), start+9)
	lines := make([]string, 0, end-start)
	for index := start; index < end; index++ {
		prefix := "  "
		style := subtleStyle
		if index == m.selected {
			prefix = "> "
			style = setupSelectedStyle
		}
		lines = append(lines, style.Render(prefix+m.models[index]))
	}
	return strings.Join(lines, "\n")
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

func modelIndex(models []string, wanted string) int {
	for index, model := range models {
		if model == wanted {
			return index
		}
	}
	return 0
}

var setupSelectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
