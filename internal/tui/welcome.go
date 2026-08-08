package tui

import (
	"context"
	"errors"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const symphonyMark = `███████╗██╗   ██╗███╗   ███╗██████╗ ██╗  ██╗ ██████╗ ███╗   ██╗██╗   ██╗
██╔════╝╚██╗ ██╔╝████╗ ████║██╔══██╗██║  ██║██╔═══██╗████╗  ██║╚██╗ ██╔╝
███████╗ ╚████╔╝ ██╔████╔██║██████╔╝███████║██║   ██║██╔██╗ ██║ ╚████╔╝
╚════██║  ╚██╔╝  ██║╚██╔╝██║██╔═══╝ ██╔══██║██║   ██║██║╚██╗██║  ╚██╔╝
███████║   ██║   ██║ ╚═╝ ██║██║     ██║  ██║╚██████╔╝██║ ╚████║   ██║
╚══════╝   ╚═╝   ╚═╝     ╚═╝╚═╝     ╚═╝  ╚═╝ ╚═════╝ ╚═╝  ╚═══╝   ╚═╝`

// Welcome displays the session splash before work begins.
func Welcome(ctx context.Context, config SetupConfig) (string, error) {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	program := tea.NewProgram(newWelcomeModel(runCtx, cancel, config), tea.WithAltScreen(), tea.WithContext(runCtx))
	result, err := program.Run()
	if err != nil {
		if runCtx.Err() != nil {
			return "", ErrCanceled
		}
		return "", err
	}
	final, ok := result.(welcomeModel)
	if !ok {
		return "", errors.New("unexpected TUI welcome model")
	}
	if final.canceled || ctx.Err() != nil {
		return "", ErrCanceled
	}
	return strings.TrimSpace(final.input.Value()), nil
}

type welcomeModel struct {
	cancel   context.CancelFunc
	config   SetupConfig
	input    textinput.Model
	canceled bool
	width    int
	height   int
}

func newWelcomeModel(_ context.Context, cancel context.CancelFunc, config SetupConfig) welcomeModel {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = "Start with a request..."
	input.TextStyle = commandTextStyle
	input.PlaceholderStyle = subtleStyle
	input.Focus()
	return welcomeModel{cancel: cancel, config: config, input: input}
}

func (m welcomeModel) Init() tea.Cmd {
	return nil
}

func (m welcomeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.Width = max(20, msg.Width-14)
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "ctrl+q", "esc":
			m.canceled = true
			m.cancel()
			return m, tea.Quit
		case "enter":
			return m, tea.Quit
		}
	}
	var command tea.Cmd
	m.input, command = m.input.Update(msg)
	return m, command
}

func (m welcomeModel) View() string {
	if m.width == 0 {
		return "Loading Symphony..."
	}
	composer := composerStyle.Width(max(20, m.width-15)).Render(welcomePromptStyle.Render("ASK  ") + m.input.View())
	content := markStyle.Render(symphonyMark) + "\n\n" +
		subtleStyle.Render(m.config.Provider+" / "+m.config.Model+"  |  "+m.config.Workspace) + "\n\n" +
		composer + "\n\n" + subtleStyle.Render("Enter starts chat  ·  Ctrl+Q quits")
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

var (
	markStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("69"))
	welcomePromptStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
)
