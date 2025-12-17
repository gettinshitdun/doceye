package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sujal/doceye/config"
)

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FF6B6B")).
			Background(lipgloss.Color("#1a1a2e")).
			Padding(0, 2)

	itemStyle = lipgloss.NewStyle().
			PaddingLeft(2)

	selectedStyle = lipgloss.NewStyle().
			PaddingLeft(0).
			Foreground(lipgloss.Color("#04B575")).
			Bold(true)

	cursorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF6B6B")).
			Bold(true)

	runningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#04B575"))

	stoppedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666"))

	detailStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			PaddingLeft(4)

	urlStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#61AFEF")).
			PaddingLeft(4)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626262"))

	containerStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#3d3d5c")).
			Padding(1, 2)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			MarginTop(1)
)

// Process tracks a running command
type Process struct {
	Cmd *exec.Cmd
	PID int
}

// KeyMap defines keybindings
type KeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Toggle key.Binding
	Quit   key.Binding
}

var DefaultKeyMap = KeyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
	),
	Toggle: key.NewBinding(
		key.WithKeys("enter", " "),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
	),
}

// Model represents the TUI state
type Model struct {
	projects  []config.Project
	processes map[int]*Process // index -> process
	cursor    int
	keys      KeyMap
	quitting  bool
}

// NewModel creates a new TUI model
func NewModel(projects []config.Project) Model {
	return Model{
		projects:  projects,
		processes: make(map[int]*Process),
		cursor:    0,
		keys:      DefaultKeyMap,
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Quit):
			m.killAll()
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

		case key.Matches(msg, m.keys.Toggle):
			m.toggleProject(m.cursor)
		}
	}

	return m, nil
}

func (m *Model) toggleProject(idx int) {
	if proc, running := m.processes[idx]; running {
		// Kill the process
		if proc.Cmd.Process != nil {
			// Kill the process group
			syscall.Kill(-proc.PID, syscall.SIGTERM)
		}
		delete(m.processes, idx)
	} else {
		// Start the process
		project := m.projects[idx]

		// Check if directory exists
		if _, err := os.Stat(project.Path); os.IsNotExist(err) {
			return
		}

		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/zsh"
		}

		cmd := exec.Command(shell, "-c", project.Command)
		cmd.Dir = project.Path
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setpgid: true,
		}

		if err := cmd.Start(); err != nil {
			return
		}

		m.processes[idx] = &Process{
			Cmd: cmd,
			PID: cmd.Process.Pid,
		}
	}
}

func (m *Model) killAll() {
	for idx, proc := range m.processes {
		if proc.Cmd.Process != nil {
			syscall.Kill(-proc.PID, syscall.SIGTERM)
		}
		delete(m.processes, idx)
	}
}

func (m *Model) isRunning(idx int) bool {
	if proc, ok := m.processes[idx]; ok {
		// Check if process is still running
		if proc.Cmd.Process != nil {
			// Try to check if process exists
			err := proc.Cmd.Process.Signal(syscall.Signal(0))
			if err != nil {
				delete(m.processes, idx)
				return false
			}
			return true
		}
	}
	return false
}

func (m *Model) runningCount() int {
	count := 0
	for idx := range m.processes {
		if m.isRunning(idx) {
			count++
		}
	}
	return count
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	// Title
	title := titleStyle.Render("doceye")
	b.WriteString(title)
	b.WriteString("\n\n")

	// Project list
	var items strings.Builder
	for i, project := range m.projects {
		cursor := "  "
		if i == m.cursor {
			cursor = cursorStyle.Render("> ")
		}

		// Status indicator
		var status string
		var name string
		running := m.isRunning(i)

		if running {
			status = runningStyle.Render("[ON] ")
			if i == m.cursor {
				name = selectedStyle.Render(project.Name)
			} else {
				name = runningStyle.Render(project.Name)
			}
		} else {
			status = stoppedStyle.Render("[--] ")
			if i == m.cursor {
				name = selectedStyle.Render(project.Name)
			} else {
				name = itemStyle.Render(project.Name)
			}
		}

		items.WriteString(cursor + status + name)

		// Show PID if running
		if running {
			if proc, ok := m.processes[i]; ok {
				pid := stoppedStyle.Render(fmt.Sprintf(" (PID: %d)", proc.PID))
				items.WriteString(pid)
			}
		}

		items.WriteString("\n")

		// Show details for selected item
		if i == m.cursor {
			path := detailStyle.Render(fmt.Sprintf("Path: %s", project.Path))
			cmd := detailStyle.Render(fmt.Sprintf("Cmd:  %s", project.Command))
			items.WriteString(path + "\n")
			items.WriteString(cmd + "\n")

			if project.Port > 0 {
				url := urlStyle.Render(fmt.Sprintf("URL:  %s", project.URL()))
				items.WriteString(url + "\n")
			}
		}

		if i < len(m.projects)-1 {
			items.WriteString("\n")
		}
	}

	b.WriteString(containerStyle.Render(items.String()))
	b.WriteString("\n")

	// Status bar
	runCount := m.runningCount()
	var statusText string
	if runCount == 0 {
		statusText = "No projects running"
	} else if runCount == 1 {
		statusText = "1 project running"
	} else {
		statusText = fmt.Sprintf("%d projects running", runCount)
	}
	b.WriteString(statusBarStyle.Render(statusText))
	b.WriteString("\n\n")

	// Help
	help := helpStyle.Render("j/k: navigate | enter/space: toggle | q: quit")
	b.WriteString(help)

	return b.String()
}

// Run starts the TUI
func Run(projects []config.Project) (*config.Project, error) {
	model := NewModel(projects)
	p := tea.NewProgram(model)

	_, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to run TUI: %w", err)
	}

	return nil, nil
}
