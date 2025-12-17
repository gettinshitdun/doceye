package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sujal/doceye/config"
)

// Styles for the TUI
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FF6B6B")).
			Background(lipgloss.Color("#1a1a2e")).
			Padding(0, 2).
			MarginBottom(1)

	itemStyle = lipgloss.NewStyle().
			PaddingLeft(2)

	selectedItemStyle = lipgloss.NewStyle().
				PaddingLeft(0).
				Foreground(lipgloss.Color("#04B575")).
				Bold(true)

	cursorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF6B6B")).
			Bold(true)

	descStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666")).
			PaddingLeft(4)

	webStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#61AFEF")).
			PaddingLeft(4)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626262")).
			MarginTop(1)

	containerStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#3d3d5c")).
			Padding(1, 2).
			MarginTop(1)
)

// KeyMap defines the keybindings for the TUI
type KeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Select key.Binding
	Quit   key.Binding
}

// DefaultKeyMap returns the default keybindings
var DefaultKeyMap = KeyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("k/up", "move up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("j/down", "move down"),
	),
	Select: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select project"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c", "esc"),
		key.WithHelp("q/esc", "quit"),
	),
}

// Model represents the TUI state
type Model struct {
	projects []config.Project
	cursor   int
	selected *config.Project
	quitting bool
	keys     KeyMap
}

// NewModel creates a new TUI model with the given projects
func NewModel(projects []config.Project) Model {
	return Model{
		projects: projects,
		cursor:   0,
		keys:     DefaultKeyMap,
	}
}

// Init implements tea.Model
func (m Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Quit):
			m.quitting = true
			return m, tea.Quit

		case key.Matches(msg, m.keys.Up):
			if m.cursor > 0 {
				m.cursor--
			}

		case key.Matches(msg, m.keys.Down):
			if m.cursor < len(m.projects)-1 {
				m.cursor++
			}

		case key.Matches(msg, m.keys.Select):
			m.selected = &m.projects[m.cursor]
			return m, tea.Quit
		}
	}

	return m, nil
}

// View implements tea.Model
func (m Model) View() string {
	if m.quitting && m.selected == nil {
		return ""
	}

	var b strings.Builder

	// Title
	title := titleStyle.Render("doceye - Project Launcher")
	b.WriteString(title)
	b.WriteString("\n\n")

	// Project list
	var items strings.Builder
	for i, project := range m.projects {
		cursor := "  "
		name := itemStyle.Render(project.Name)

		if i == m.cursor {
			cursor = cursorStyle.Render("> ")
			name = selectedItemStyle.Render(project.Name)
		}

		items.WriteString(cursor + name + "\n")

		// Show details for selected item
		if i == m.cursor {
			path := descStyle.Render(fmt.Sprintf("Path: %s", project.Path))
			cmd := descStyle.Render(fmt.Sprintf("Cmd:  %s", project.Command))
			items.WriteString(path + "\n")
			items.WriteString(cmd + "\n")

			// Show URL for projects with a port
			if project.Port > 0 {
				url := webStyle.Render(fmt.Sprintf("URL:  %s", project.URL()))
				items.WriteString(url + "\n")
			}
		}

		if i < len(m.projects)-1 {
			items.WriteString("\n")
		}
	}

	b.WriteString(containerStyle.Render(items.String()))
	b.WriteString("\n")

	// Help
	help := helpStyle.Render("k/up • j/down • enter select • q/esc quit")
	b.WriteString(help)

	return b.String()
}

// Selected returns the selected project, or nil if none was selected
func (m Model) Selected() *config.Project {
	return m.selected
}

// Run starts the TUI and returns the selected project
func Run(projects []config.Project) (*config.Project, error) {
	model := NewModel(projects)
	p := tea.NewProgram(model)

	finalModel, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to run TUI: %w", err)
	}

	return finalModel.(Model).Selected(), nil
}
