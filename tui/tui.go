package tui

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sujal/doceye/config"
)

// Status represents the state of a project
type Status int

const (
	StatusStopped Status = iota
	StatusStarting
	StatusRunning
	StatusFailed
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

	startingStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E5C07B"))

	stoppedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666"))

	failedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E06C75"))

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

	logTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#61AFEF")).
			MarginTop(1)

	logStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#3d3d5c")).
			Padding(0, 1)

	errorMsgStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E06C75")).
			PaddingLeft(4)
)

// PortInfo contains information about a process using a port
type PortInfo struct {
	PID     string
	Command string
	User    string
}

// LogBuffer stores logs for a process
type LogBuffer struct {
	lines []string
	mu    sync.Mutex
}

func NewLogBuffer() *LogBuffer {
	return &LogBuffer{
		lines: make([]string, 0),
	}
}

func (lb *LogBuffer) Write(p []byte) (n int, err error) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	text := string(p)
	newLines := strings.Split(text, "\n")
	for _, line := range newLines {
		if line != "" {
			lb.lines = append(lb.lines, line)
			// Keep only last 1000 lines
			if len(lb.lines) > 1000 {
				lb.lines = lb.lines[len(lb.lines)-1000:]
			}
		}
	}
	return len(p), nil
}

func (lb *LogBuffer) GetLines() []string {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	result := make([]string, len(lb.lines))
	copy(result, lb.lines)
	return result
}

func (lb *LogBuffer) GetContent() string {
	lines := lb.GetLines()
	return strings.Join(lines, "\n")
}

// Process tracks a running command
type Process struct {
	Cmd              *exec.Cmd
	PID              int
	Status           Status
	Logs             *LogBuffer
	ExitCode         int
	ProcessDone      bool // true if the process has exited
	PortCheckRetries int  // count of port check failures after process exit
}

// Messages for async operations
type portCheckMsg struct {
	idx  int
	isUp bool
}

type processExitMsg struct {
	idx      int
	exitCode int
	err      error
}

type logUpdateMsg struct{}

type tickMsg time.Time

// KeyMap defines keybindings
type KeyMap struct {
	Up         key.Binding
	Down       key.Binding
	Toggle     key.Binding
	ToggleLogs key.Binding
	ScrollUp   key.Binding
	ScrollDown key.Binding
	Quit       key.Binding
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
	ToggleLogs: key.NewBinding(
		key.WithKeys("l"),
	),
	ScrollUp: key.NewBinding(
		key.WithKeys("ctrl+u"),
	),
	ScrollDown: key.NewBinding(
		key.WithKeys("ctrl+d"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c", "esc"),
	),
}

// Model represents the TUI state
type Model struct {
	projects      []config.Project
	processes     map[int]*Process
	cursor        int
	keys          KeyMap
	quitting      bool
	spinner       int
	showLogs      bool
	viewport      viewport.Model
	width         int
	height        int
	errorMessages map[int]string
	program       *tea.Program
}

var spinnerFrames = []string{".", "..", "..."}

// NewModel creates a new TUI model
func NewModel(projects []config.Project) Model {
	vp := viewport.New(80, 10)
	vp.Style = logStyle

	return Model{
		projects:      projects,
		processes:     make(map[int]*Process),
		cursor:        0,
		keys:          DefaultKeyMap,
		spinner:       0,
		showLogs:      false,
		viewport:      vp,
		width:         80,
		height:        24,
		errorMessages: make(map[int]string),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(tickCmd(), logUpdateCmd())
}

func tickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func logUpdateCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return logUpdateMsg{}
	})
}

func checkPortCmd(idx int, port int) tea.Cmd {
	return func() tea.Msg {
		addr := fmt.Sprintf("localhost:%d", port)
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err != nil {
			return portCheckMsg{idx: idx, isUp: false}
		}
		conn.Close()
		return portCheckMsg{idx: idx, isUp: true}
	}
}

func waitForProcessCmd(idx int, cmd *exec.Cmd) tea.Cmd {
	return func() tea.Msg {
		err := cmd.Wait()
		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = -1
			}
		}
		return processExitMsg{idx: idx, exitCode: exitCode, err: err}
	}
}

// checkPortInUse checks if a port is already in use and returns info about the process
func checkPortInUse(port int) (bool, *PortInfo) {
	// First, use lsof to check if anything is listening on the port
	// This is more reliable than trying to connect
	cmd := exec.Command("lsof", "-i", fmt.Sprintf(":%d", port), "-P", "-n", "-sTCP:LISTEN")
	output, err := cmd.Output()
	if err != nil || len(output) == 0 {
		// No process listening on this port
		return false, nil
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) < 2 {
		return false, nil
	}

	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}

		fields := regexp.MustCompile(`\s+`).Split(strings.TrimSpace(line), -1)
		if len(fields) >= 3 {
			return true, &PortInfo{
				Command: fields[0],
				PID:     fields[1],
				User:    fields[2],
			}
		}
	}

	return false, nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = msg.Width - 4
		m.viewport.Height = 12
		return m, nil

	case logUpdateMsg:
		if m.showLogs {
			if proc, ok := m.processes[m.cursor]; ok {
				content := proc.Logs.GetContent()
				m.viewport.SetContent(content)
				m.viewport.GotoBottom()
			}
		}
		return m, logUpdateCmd()

	case tickMsg:
		m.spinner = (m.spinner + 1) % len(spinnerFrames)

		for idx, proc := range m.processes {
			project := m.projects[idx]
			// Keep checking port for Starting status, or if process exited but we're waiting
			if project.Port > 0 && (proc.Status == StatusStarting || (proc.ProcessDone && proc.Status != StatusFailed && proc.Status != StatusRunning)) {
				cmds = append(cmds, checkPortCmd(idx, project.Port))
			}
		}
		cmds = append(cmds, tickCmd())
		return m, tea.Batch(cmds...)

	case portCheckMsg:
		if proc, ok := m.processes[msg.idx]; ok {
			project := m.projects[msg.idx]
			if msg.isUp {
				// Port is up - service is running regardless of process state
				// (handles daemonized processes)
				if proc.Status == StatusStarting || proc.Status == StatusFailed {
					proc.Status = StatusRunning
					proc.PortCheckRetries = 0
					delete(m.errorMessages, msg.idx)
				}
			} else if proc.ProcessDone && proc.Status == StatusStarting {
				// Process has exited and port still not up
				// Give it more time (30 retries * 500ms = 15 seconds grace period)
				proc.PortCheckRetries++
				if proc.PortCheckRetries >= 30 {
					proc.Status = StatusFailed
					if proc.ExitCode != 0 {
						m.errorMessages[msg.idx] = fmt.Sprintf("Process exited with code %d", proc.ExitCode)
					} else {
						m.errorMessages[msg.idx] = fmt.Sprintf("Process exited but port %d never came up", project.Port)
					}
				}
			}
		}
		return m, nil

	case processExitMsg:
		if proc, ok := m.processes[msg.idx]; ok {
			proc.ExitCode = msg.exitCode
			proc.ProcessDone = true
			project := m.projects[msg.idx]

			// For projects with a port, don't immediately mark as failed
			// Let the port check determine if it's actually running (daemonized process)
			if project.Port > 0 {
				// Check port one more time before deciding
				addr := fmt.Sprintf("localhost:%d", project.Port)
				conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
				if err == nil {
					conn.Close()
					// Port is up! Process daemonized successfully
					proc.Status = StatusRunning
					return m, nil
				}
				// Port not up yet - if we were starting, keep waiting for port checks
				// If exit code != 0, mark as failed immediately
				if msg.exitCode != 0 {
					proc.Status = StatusFailed
					m.errorMessages[msg.idx] = fmt.Sprintf("Process exited with code %d", msg.exitCode)
				}
				// If exit code == 0 but port not up, port check will handle it
			} else {
				// No port to check - use exit code
				if msg.exitCode != 0 {
					proc.Status = StatusFailed
					m.errorMessages[msg.idx] = fmt.Sprintf("Process exited with code %d", msg.exitCode)
				} else {
					proc.Status = StatusStopped
					delete(m.processes, msg.idx)
				}
			}
		}
		return m, nil

	case tea.KeyMsg:
		if m.showLogs {
			switch {
			case key.Matches(msg, m.keys.Quit):
				m.showLogs = false
				return m, nil
			case key.Matches(msg, m.keys.ToggleLogs):
				m.showLogs = false
				return m, nil
			case key.Matches(msg, m.keys.ScrollUp):
				m.viewport.HalfViewUp()
				return m, nil
			case key.Matches(msg, m.keys.ScrollDown):
				m.viewport.HalfViewDown()
				return m, nil
			case msg.Type == tea.KeyUp:
				m.viewport.LineUp(1)
				return m, nil
			case msg.Type == tea.KeyDown:
				m.viewport.LineDown(1)
				return m, nil
			}
			return m, nil
		}

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
			cmd := m.toggleProject(m.cursor)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)

		case key.Matches(msg, m.keys.ToggleLogs):
			if proc, ok := m.processes[m.cursor]; ok {
				m.showLogs = true
				content := proc.Logs.GetContent()
				m.viewport.SetContent(content)
				m.viewport.GotoBottom()
			}
		}
	}

	return m, nil
}

func (m *Model) toggleProject(idx int) tea.Cmd {
	delete(m.errorMessages, idx)

	if proc, exists := m.processes[idx]; exists {
		if proc.Cmd.Process != nil {
			syscall.Kill(-proc.PID, syscall.SIGTERM)
		}
		delete(m.processes, idx)
		m.showLogs = false
		return nil
	}

	project := m.projects[idx]

	if _, err := os.Stat(project.Path); os.IsNotExist(err) {
		m.errorMessages[idx] = fmt.Sprintf("Directory not found: %s", project.Path)
		return nil
	}

	if project.Port > 0 {
		inUse, portInfo := checkPortInUse(project.Port)
		if inUse {
			if portInfo != nil {
				m.errorMessages[idx] = fmt.Sprintf("Port %d in use by %s (PID: %s, User: %s)",
					project.Port, portInfo.Command, portInfo.PID, portInfo.User)
			} else {
				m.errorMessages[idx] = fmt.Sprintf("Port %d is already in use", project.Port)
			}
			return nil
		}
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

	logBuffer := NewLogBuffer()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		m.errorMessages[idx] = fmt.Sprintf("Failed to create stdout pipe: %v", err)
		return nil
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		m.errorMessages[idx] = fmt.Sprintf("Failed to create stderr pipe: %v", err)
		return nil
	}

	if err := cmd.Start(); err != nil {
		m.errorMessages[idx] = fmt.Sprintf("Failed to start: %v", err)
		return nil
	}

	go captureOutput(stdout, logBuffer)
	go captureOutput(stderr, logBuffer)

	status := StatusRunning
	if project.Port > 0 {
		status = StatusStarting
	}

	m.processes[idx] = &Process{
		Cmd:              cmd,
		PID:              cmd.Process.Pid,
		Status:           status,
		Logs:             logBuffer,
		ExitCode:         0,
		ProcessDone:      false,
		PortCheckRetries: 0,
	}

	// Start watching for process exit
	var cmds []tea.Cmd
	cmds = append(cmds, waitForProcessCmd(idx, cmd))

	if project.Port > 0 {
		cmds = append(cmds, checkPortCmd(idx, project.Port))
	}

	return tea.Batch(cmds...)
}

func captureOutput(r io.Reader, lb *LogBuffer) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		lb.Write([]byte(scanner.Text() + "\n"))
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

func (m *Model) getStatus(idx int) Status {
	if proc, ok := m.processes[idx]; ok {
		return proc.Status
	}
	return StatusStopped
}

func (m *Model) countByStatus() (starting, running, failed int) {
	for idx := range m.processes {
		status := m.getStatus(idx)
		switch status {
		case StatusStarting:
			starting++
		case StatusRunning:
			running++
		case StatusFailed:
			failed++
		}
	}
	return
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	title := titleStyle.Render("doceye")
	b.WriteString(title)
	b.WriteString("\n\n")

	var items strings.Builder
	for i, project := range m.projects {
		cursor := "  "
		if i == m.cursor {
			cursor = cursorStyle.Render("> ")
		}

		status := m.getStatus(i)
		var statusText string
		var name string

		switch status {
		case StatusRunning:
			statusText = runningStyle.Render("[ON]  ")
			if i == m.cursor {
				name = selectedStyle.Render(project.Name)
			} else {
				name = runningStyle.Render(project.Name)
			}
		case StatusStarting:
			statusText = startingStyle.Render("[" + spinnerFrames[m.spinner] + "]  ")
			if i == m.cursor {
				name = selectedStyle.Render(project.Name)
			} else {
				name = startingStyle.Render(project.Name)
			}
		case StatusFailed:
			statusText = failedStyle.Render("[XX]  ")
			if i == m.cursor {
				name = selectedStyle.Render(project.Name)
			} else {
				name = failedStyle.Render(project.Name)
			}
		default:
			statusText = stoppedStyle.Render("[--]  ")
			if i == m.cursor {
				name = selectedStyle.Render(project.Name)
			} else {
				name = itemStyle.Render(project.Name)
			}
		}

		items.WriteString(cursor + statusText + name)

		if proc, ok := m.processes[i]; ok && status != StatusStopped {
			if status == StatusFailed {
				info := stoppedStyle.Render(fmt.Sprintf(" (exit: %d)", proc.ExitCode))
				items.WriteString(info)
			} else {
				pid := stoppedStyle.Render(fmt.Sprintf(" (PID: %d)", proc.PID))
				items.WriteString(pid)
			}
		}

		items.WriteString("\n")

		if i == m.cursor {
			path := detailStyle.Render(fmt.Sprintf("Path: %s", project.Path))
			cmd := detailStyle.Render(fmt.Sprintf("Cmd:  %s", project.Command))
			items.WriteString(path + "\n")
			items.WriteString(cmd + "\n")

			if project.Port > 0 {
				url := urlStyle.Render(fmt.Sprintf("URL:  %s", project.URL()))
				items.WriteString(url + "\n")
			}

			if errMsg, ok := m.errorMessages[i]; ok {
				items.WriteString(errorMsgStyle.Render("Error: "+errMsg) + "\n")
			}
		}

		if i < len(m.projects)-1 {
			items.WriteString("\n")
		}
	}

	b.WriteString(containerStyle.Render(items.String()))
	b.WriteString("\n")

	if m.showLogs {
		if proc, ok := m.processes[m.cursor]; ok {
			projectName := m.projects[m.cursor].Name
			var logTitle string
			if proc.Status == StatusFailed {
				logTitle = logTitleStyle.Render(fmt.Sprintf("Logs: %s (FAILED, exit: %d)", projectName, proc.ExitCode))
			} else {
				logTitle = logTitleStyle.Render(fmt.Sprintf("Logs: %s (PID: %d)", projectName, proc.PID))
			}
			b.WriteString(logTitle)
			b.WriteString("\n")
			b.WriteString(m.viewport.View())
			b.WriteString("\n")
		}
	}

	starting, running, failed := m.countByStatus()
	var statusParts []string
	if running > 0 {
		statusParts = append(statusParts, fmt.Sprintf("%d running", running))
	}
	if starting > 0 {
		statusParts = append(statusParts, fmt.Sprintf("%d starting", starting))
	}
	if failed > 0 {
		statusParts = append(statusParts, failedStyle.Render(fmt.Sprintf("%d failed", failed)))
	}

	var statusText string
	if len(statusParts) == 0 {
		statusText = "No projects running"
	} else {
		statusText = strings.Join(statusParts, ", ")
	}
	b.WriteString(statusBarStyle.Render(statusText))
	b.WriteString("\n\n")

	var helpText string
	if m.showLogs {
		helpText = "j/k: scroll | ctrl+u/d: page | l/q/esc: close logs"
	} else {
		helpText = "j/k: navigate | enter/space: toggle | l: logs | q: quit"
	}
	help := helpStyle.Render(helpText)
	b.WriteString(help)

	return b.String()
}

// Run starts the TUI
func Run(projects []config.Project) (*config.Project, error) {
	model := NewModel(projects)
	p := tea.NewProgram(model, tea.WithAltScreen())

	_, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to run TUI: %w", err)
	}

	return nil, nil
}
