package tui

import (
	"context"
	"errors"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// WaitForKurrent shows startup progress until the local database is ready.
func WaitForKurrent(ctx context.Context, start func(context.Context) error) error {
	if start == nil {
		return errors.New("KurrentDB starter is required")
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	program := tea.NewProgram(newWaitModel(runCtx, cancel, start), tea.WithAltScreen(), tea.WithContext(runCtx))
	result, err := program.Run()
	if err != nil {
		if runCtx.Err() != nil {
			return ErrCanceled
		}
		return err
	}
	final, ok := result.(waitModel)
	if !ok {
		return errors.New("unexpected TUI startup model")
	}
	if final.canceled || ctx.Err() != nil {
		return ErrCanceled
	}
	return final.err
}

type kurrentStartedMsg struct {
	err error
}

type waitModel struct {
	ctx      context.Context
	cancel   context.CancelFunc
	start    func(context.Context) error
	spinner  spinner.Model
	canceled bool
	err      error
	width    int
	height   int
}

func newWaitModel(ctx context.Context, cancel context.CancelFunc, start func(context.Context) error) waitModel {
	indicator := spinner.New(spinner.WithSpinner(spinner.Dot))
	indicator.Style = titleStyle
	return waitModel{ctx: ctx, cancel: cancel, start: start, spinner: indicator}
}

func (m waitModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, func() tea.Msg {
		return kurrentStartedMsg{err: m.start(m.ctx)}
	})
}

func (m waitModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "ctrl+q" || msg.String() == "esc" {
			m.canceled = true
			m.cancel()
			return m, tea.Quit
		}
	case kurrentStartedMsg:
		m.err = msg.err
		return m, tea.Quit
	case spinner.TickMsg:
		var command tea.Cmd
		m.spinner, command = m.spinner.Update(msg)
		return m, command
	}
	return m, nil
}

func (m waitModel) View() string {
	if m.width == 0 {
		return "Starting Symphony..."
	}
	content := titleStyle.Render("SYMPHONY") + "\n\n" + m.spinner.View() + " Starting local KurrentDB...\n" + subtleStyle.Render("This can take a moment the first time the container image is pulled.") + "\n\n" + subtleStyle.Render("Ctrl+C cancels.")
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}
