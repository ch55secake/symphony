package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

var activeTheme = "default"

func themeNames() []string {
	return []string{"default", "contrast", "mono"}
}

func currentTheme() string {
	return activeTheme
}

func validTheme(name string) bool {
	for _, candidate := range themeNames() {
		if name == candidate {
			return true
		}
	}
	return false
}

func themeIndex(name string) int {
	for index, candidate := range themeNames() {
		if name == candidate {
			return index
		}
	}
	return 0
}

// SetTheme applies a built-in theme to subsequently rendered TUI views.
func SetTheme(name string) error {
	if !validTheme(name) {
		return fmt.Errorf("unknown theme %q", name)
	}
	activeTheme = name
	accent, assistant, warning, muted, text := "69", "212", "214", "244", "252"
	switch name {
	case "contrast":
		accent, assistant, warning, muted, text = "51", "213", "226", "250", "255"
	case "mono":
		accent, assistant, warning, muted, text = "255", "255", "255", "245", "255"
	}
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(accent))
	subtleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(muted))
	userStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(accent))
	assistantStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(assistant))
	activityStyle = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color(muted))
	approvalStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(warning))
	errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	commandPromptStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(accent))
	commandTextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(text))
	composerStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(accent)).Padding(0, 1)
	suggestionStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(muted)).Padding(0, 1)
	markStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(accent))
	welcomePromptStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(assistant))
	setupSelectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(assistant))
	return nil
}
