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

	"path/filepath"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fsnotify/fsnotify"
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

	reloadStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E5C07B"))
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
	ProcessDone      bool
	PortCheckRetries int
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
type configChangedMsg struct {
	newConfig *config.Config
}

// KeyMap defines keybindings
type KeyMap struct {
	Up         key.Binding
	Down       key.Binding
	Toggle     key.Binding
	ToggleLogs key.Binding
	ScrollUp   key.Binding
	ScrollDown key.Binding
	Reload     key.Binding
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
		key.WithKeys("ctrl+u", "pgup"),
	),
	ScrollDown: key.NewBinding(
		key.WithKeys("ctrl+d", "pgdown"),
	),
	Reload: key.NewBinding(
		key.WithKeys("r"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c", "esc"),
	),
}

// Model represents the TUI state
type Model struct {
	projects         []config.Project
	processes        map[int]*Process
	cursor           int
	keys             KeyMap
	quitting         bool
	spinner          int
	showLogs         bool
	viewport         viewport.Model
	width            int
	height           int
	errorMessages    map[int]string
	configPath       string
	configReloaded   bool
	configChangeChan chan *config.Config
}

var spinnerFrames = []string{".", "..", "..."}

// NewModel creates a new TUI model
func NewModel(projects []config.Project, configPath string) Model {
	vp := viewport.New(80, 12)
	vp.Style = logStyle

	// Create channel for config changes
	changeChan := make(chan *config.Config, 1)

	// Start file watcher in background
	go watchConfigFile(configPath, changeChan)

	return Model{
		projects:         projects,
		processes:        make(map[int]*Process),
		cursor:           0,
		keys:             DefaultKeyMap,
		spinner:          0,
		showLogs:         false,
		viewport:         vp,
		width:            80,
		height:           24,
		errorMessages:    make(map[int]string),
		configPath:       configPath,
		configReloaded:   false,
		configChangeChan: changeChan,
	}
}

// watchConfigFile watches the config file's directory for changes (handles vim/vscode rename-on-save)
func watchConfigFile(configPath string, changeChan chan *config.Config) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return
	}
	defer watcher.Close()

	// Watch the directory, not the file
	dir := filepath.Dir(configPath)
	filename := filepath.Base(configPath)

	err = watcher.Add(dir)
	if err != nil {
		return
	}

	// Debounce timer to avoid multiple reloads
	var debounceTimer *time.Timer

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			// Check if this event is for our config file
			if filepath.Base(event.Name) != filename {
				continue
			}

			// Handle Write, Create, or Rename events
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) != 0 {
				// Debounce: wait for all writes to complete
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.AfterFunc(200*time.Millisecond, func() {
					newConfig, err := config.Load(configPath)
					if err == nil {
						select {
						case changeChan <- newConfig:
						default:
						}
					}
				})
			}

		case _, ok := <-watcher.Errors:
			if !ok {
				return
			}
		}
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(tickCmd(), logUpdateCmd(), checkConfigChangeCmd(m.configChangeChan))
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

func checkConfigChangeCmd(changeChan chan *config.Config) tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
		select {
		case newConfig := <-changeChan:
			return configChangedMsg{newConfig: newConfig}
		default:
			return checkConfigMsg{}
		}
	})
}

type checkConfigMsg struct{}

type delayedRestartMsg struct {
	indices []int
}

func delayedRestartCmd(indices []int) tea.Cmd {
	return tea.Tick(1*time.Second, func(t time.Time) tea.Msg {
		return delayedRestartMsg{indices: indices}
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

func checkPortInUse(port int) (bool, *PortInfo) {
	cmd := exec.Command("lsof", "-i", fmt.Sprintf(":%d", port), "-P", "-n", "-sTCP:LISTEN")
	output, err := cmd.Output()
	if err != nil || len(output) == 0 {
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

// killProcessOnPort kills any process listening on the given port
func killProcessOnPort(port int) {
	cmd := exec.Command("lsof", "-i", fmt.Sprintf(":%d", port), "-P", "-n", "-sTCP:LISTEN", "-t")
	output, err := cmd.Output()
	if err != nil || len(output) == 0 {
		return
	}

	pids := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, pid := range pids {
		pid = strings.TrimSpace(pid)
		if pid != "" {
			// Kill the process
			exec.Command("kill", "-9", pid).Run()
		}
	}

	// Give it a moment to release the port
	time.Sleep(500 * time.Millisecond)
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

	case checkConfigMsg:
		// Continue checking for config changes
		return m, checkConfigChangeCmd(m.configChangeChan)

	case configChangedMsg:
		// Config file changed - reload and restart changed projects
		m.configReloaded = true
		oldProjects := m.projects
		m.projects = msg.newConfig.Projects

		// Collect indices of projects that need restart
		var projectsToRestart []int

		// Check each project for changes
		for i, newProj := range m.projects {
			if i < len(oldProjects) {
				oldProj := oldProjects[i]
				// If project config changed and it's running, restart it
				if proc, running := m.processes[i]; running {
					if newProj.Command != oldProj.Command || newProj.Path != oldProj.Path || newProj.Port != oldProj.Port {
						// Kill old process
						if proc.Cmd.Process != nil {
							syscall.Kill(-proc.PID, syscall.SIGTERM)
						}
						// Also kill anything still using the port (e.g., Docker containers)
						if oldProj.Port > 0 {
							killProcessOnPort(oldProj.Port)
						}
						delete(m.processes, i)
						delete(m.errorMessages, i)
						projectsToRestart = append(projectsToRestart, i)
					}
				}
			}
		}

		// Schedule delayed restart to allow ports to be released
		if len(projectsToRestart) > 0 {
			cmds = append(cmds, delayedRestartCmd(projectsToRestart))
		}

		// Continue watching for config changes
		cmds = append(cmds, checkConfigChangeCmd(m.configChangeChan))

		// Clear the reload indicator after 2 seconds
		cmds = append(cmds, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return clearReloadMsg{}
		}))

		return m, tea.Batch(cmds...)

	case clearReloadMsg:
		m.configReloaded = false
		return m, nil

	case delayedRestartMsg:
		// Restart projects after delay (allows ports to be released)
		var cmds []tea.Cmd
		for _, idx := range msg.indices {
			cmd := m.toggleProject(idx)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if len(cmds) > 0 {
			return m, tea.Batch(cmds...)
		}
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
				if proc.Status == StatusStarting || proc.Status == StatusFailed {
					proc.Status = StatusRunning
					proc.PortCheckRetries = 0
					delete(m.errorMessages, msg.idx)
				}
			} else if proc.ProcessDone && proc.Status == StatusStarting {
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

			if project.Port > 0 {
				addr := fmt.Sprintf("localhost:%d", project.Port)
				conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
				if err == nil {
					conn.Close()
					proc.Status = StatusRunning
					return m, nil
				}
				if msg.exitCode != 0 {
					proc.Status = StatusFailed
					m.errorMessages[msg.idx] = fmt.Sprintf("Process exited with code %d", msg.exitCode)
				}
			} else {
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
			case key.Matches(msg, m.keys.Quit), key.Matches(msg, m.keys.ToggleLogs):
				m.showLogs = false
				return m, nil
			case key.Matches(msg, m.keys.ScrollUp):
				m.viewport.HalfViewUp()
				return m, nil
			case key.Matches(msg, m.keys.ScrollDown):
				m.viewport.HalfViewDown()
				return m, nil
			case key.Matches(msg, m.keys.Up):
				m.viewport.LineUp(3)
				return m, nil
			case key.Matches(msg, m.keys.Down):
				m.viewport.LineDown(3)
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

		case key.Matches(msg, m.keys.Reload):
			// Manual reload
			newConfig, err := config.Load(m.configPath)
			if err == nil {
				return m, func() tea.Msg {
					return configChangedMsg{newConfig: newConfig}
				}
			}
		}
	}

	return m, nil
}

type clearReloadMsg struct{}

func (m *Model) toggleProject(idx int) tea.Cmd {
	delete(m.errorMessages, idx)

	if proc, exists := m.processes[idx]; exists {
		project := m.projects[idx]
		if proc.Cmd.Process != nil {
			syscall.Kill(-proc.PID, syscall.SIGTERM)
		}
		// Also kill anything using the port (e.g., Docker containers)
		if project.Port > 0 {
			killProcessOnPort(project.Port)
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

	var resultCmds []tea.Cmd
	resultCmds = append(resultCmds, waitForProcessCmd(idx, cmd))

	if project.Port > 0 {
		resultCmds = append(resultCmds, checkPortCmd(idx, project.Port))
	}

	return tea.Batch(resultCmds...)
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

	// Title with reload indicator
	titleText := "doceye"
	if m.configReloaded {
		titleText = "doceye " + reloadStyle.Render("[config reloaded]")
	}
	title := titleStyle.Render(titleText)
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
		helpText = "j/k: scroll | ctrl+u/d: page | l/esc: close logs"
	} else {
		helpText = "j/k: navigate | enter/space: toggle | l: logs | r: reload | q: quit"
	}
	help := helpStyle.Render(helpText)
	b.WriteString(help)

	return b.String()
}

// Run starts the TUI
func Run(projects []config.Project, configPath string) (*config.Project, error) {
	model := NewModel(projects, configPath)
	p := tea.NewProgram(model, tea.WithAltScreen())

	_, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to run TUI: %w", err)
	}

	return nil, nil
}

