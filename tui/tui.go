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
	"github.com/gettinshitdun/doceye/config"
)

// Status represents the state of a project
type Status int

const (
	StatusStopped Status = iota
	StatusStarting
	StatusRunning
	StatusFailed
)

// Color palette - Terminal-native, high contrast
var (
	// Core colors - using ANSI-friendly hex
	colorGreen  = lipgloss.Color("#98c379")
	colorYellow = lipgloss.Color("#e5c07b")
	colorRed    = lipgloss.Color("#e06c75")
	colorBlue   = lipgloss.Color("#61afef")
	colorCyan   = lipgloss.Color("#56b6c2")
	colorPurple = lipgloss.Color("#c678dd")
	colorOrange = lipgloss.Color("#d19a66")

	// Grayscale
	colorWhite   = lipgloss.Color("#abb2bf")
	colorGray    = lipgloss.Color("#5c6370")
	colorDark    = lipgloss.Color("#3e4451")
	colorDarker  = lipgloss.Color("#282c34")
	colorDarkest = lipgloss.Color("#21252b")
)

// Styles
var (
	// ═══ Header ═══
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorCyan)

	versionStyle = lipgloss.NewStyle().
			Foreground(colorGray)

	// ═══ Table/List ═══
	tableHeaderStyle = lipgloss.NewStyle().
				Foreground(colorGray).
				Bold(true)

	selectedRowStyle = lipgloss.NewStyle().
				Background(colorDark)

	// ═══ Status ═══
	statusRunning = lipgloss.NewStyle().
			Foreground(colorGreen).
			Bold(true)

	statusStarting = lipgloss.NewStyle().
			Foreground(colorYellow)

	statusStopped = lipgloss.NewStyle().
			Foreground(colorGray)

	statusFailed = lipgloss.NewStyle().
			Foreground(colorRed)

	// ═══ Text hierarchy ═══
	primaryText = lipgloss.NewStyle().
			Foreground(colorWhite)

	secondaryText = lipgloss.NewStyle().
			Foreground(colorGray)

	mutedText = lipgloss.NewStyle().
			Foreground(colorDark)

	highlightText = lipgloss.NewStyle().
			Foreground(colorCyan)

	errorText = lipgloss.NewStyle().
			Foreground(colorRed)

	warningText = lipgloss.NewStyle().
			Foreground(colorYellow)

	successText = lipgloss.NewStyle().
			Foreground(colorGreen)

	linkText = lipgloss.NewStyle().
			Foreground(colorBlue).
			Underline(true)

	// ═══ Badges ═══
	badgeRunning = lipgloss.NewStyle().
			Foreground(colorDarkest).
			Background(colorGreen).
			Padding(0, 1)

	badgeStarting = lipgloss.NewStyle().
			Foreground(colorDarkest).
			Background(colorYellow).
			Padding(0, 1)

	badgeFailed = lipgloss.NewStyle().
			Foreground(colorDarkest).
			Background(colorRed).
			Padding(0, 1)

	badgeStopped = lipgloss.NewStyle().
			Foreground(colorWhite).
			Background(colorDark).
			Padding(0, 1)

	// ═══ Panels ═══
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorDark).
			Padding(0, 1)

	panelSelectedStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorCyan).
				Padding(0, 1)

	panelTitleStyle = lipgloss.NewStyle().
			Foreground(colorCyan).
			Bold(true)

	// ═══ Details ═══
	labelStyle = lipgloss.NewStyle().
			Foreground(colorGray).
			Width(8)

	valueStyle = lipgloss.NewStyle().
			Foreground(colorWhite)

	// ═══ Help bar ═══
	helpKeyStyle = lipgloss.NewStyle().
			Foreground(colorYellow).
			Bold(true)

	helpDescStyle = lipgloss.NewStyle().
			Foreground(colorGray)

	helpSepStyle = lipgloss.NewStyle().
			Foreground(colorDark)

	// ═══ Search ═══
	searchBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorCyan).
			Padding(0, 1)

	searchPromptStyle = lipgloss.NewStyle().
				Foreground(colorCyan).
				Bold(true)

	searchQueryStyle = lipgloss.NewStyle().
				Foreground(colorWhite)

	searchPlaceholderStyle = lipgloss.NewStyle().
				Foreground(colorGray).
				Italic(true)

	// ═══ Misc ═══
	dimStyle = lipgloss.NewStyle().
			Foreground(colorDark)

	accentStyle = lipgloss.NewStyle().
			Foreground(colorCyan)
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
	StartTime        time.Time
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
type restartProjectMsg struct {
	idx int
}

// KeyMap defines keybindings
type KeyMap struct {
	Up         key.Binding
	Down       key.Binding
	Toggle     key.Binding
	ToggleLogs key.Binding
	ScrollUp   key.Binding
	ScrollDown key.Binding
	Restart    key.Binding
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
	Restart: key.NewBinding(
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
	// Search
	searchMode   bool
	searchQuery  string
	filteredIdxs []int // indices into projects slice that match search
}

var spinnerFrames = []string{".", "..", "..."}

// formatUptime formats duration in a compact human-readable format
func formatUptime(d time.Duration) string {
	if d < time.Second {
		return "<1s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%ds", m, s)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%dm", h, m)
}

// truncateString truncates a string to maxLen, adding ellipsis
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// NewModel creates a new TUI model
func NewModel(projects []config.Project, configPath string) Model {
	vp := viewport.New(80, 12)

	// Create channel for config changes
	changeChan := make(chan *config.Config, 1)

	// Start file watcher in background
	go watchConfigFile(configPath, changeChan)

	// Initialize filtered indices to all projects
	filteredIdxs := make([]int, len(projects))
	for i := range projects {
		filteredIdxs[i] = i
	}

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
		searchMode:       false,
		searchQuery:      "",
		filteredIdxs:     filteredIdxs,
	}
}

// updateFilter updates the filtered indices based on search query
func (m *Model) updateFilter() {
	if m.searchQuery == "" {
		// No search query, show all projects
		m.filteredIdxs = make([]int, len(m.projects))
		for i := range m.projects {
			m.filteredIdxs[i] = i
		}
	} else {
		// Filter projects by name (case-insensitive)
		m.filteredIdxs = nil
		query := strings.ToLower(m.searchQuery)
		for i, p := range m.projects {
			if strings.Contains(strings.ToLower(p.Name), query) ||
				strings.Contains(strings.ToLower(p.Command), query) ||
				strings.Contains(strings.ToLower(p.Path), query) {
				m.filteredIdxs = append(m.filteredIdxs, i)
			}
		}
	}

	// Reset cursor if out of bounds
	if m.cursor >= len(m.filteredIdxs) {
		m.cursor = max(0, len(m.filteredIdxs)-1)
	}
}

// getSelectedProjectIndex returns the actual project index for the current cursor position
func (m Model) getSelectedProjectIndex() int {
	if len(m.filteredIdxs) == 0 {
		return -1
	}
	if m.cursor >= len(m.filteredIdxs) {
		return m.filteredIdxs[len(m.filteredIdxs)-1]
	}
	return m.filteredIdxs[m.cursor]
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
	// Stop any Docker containers using this port (safe no-op if none exist)
	cmd := exec.Command("docker", "ps", "--filter", fmt.Sprintf("publish=%d", port), "-q")
	if output, err := cmd.Output(); err == nil && len(output) > 0 {
		for _, id := range strings.Split(strings.TrimSpace(string(output)), "\n") {
			if id = strings.TrimSpace(id); id != "" {
				exec.Command("docker", "stop", "-t", "2", id).Run()
			}
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

		// Re-apply filter with new projects
		m.updateFilter()

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

		// Check ports for starting/running processes
		for idx, proc := range m.processes {
			project := m.projects[idx]
			if project.Port > 0 && (proc.Status == StatusStarting || (proc.ProcessDone && proc.Status != StatusFailed && proc.Status != StatusRunning)) {
				cmds = append(cmds, checkPortCmd(idx, project.Port))
			}
		}

		// Use faster tick for starting processes, slower for others
		hasStarting := false
		for _, proc := range m.processes {
			if proc.Status == StatusStarting {
				hasStarting = true
				break
			}
		}

		if hasStarting {
			// Check more frequently (200ms) when processes are starting
			cmds = append(cmds, tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
				return tickMsg(t)
			}))
		} else {
			// Normal tick (500ms) when no processes are starting
			cmds = append(cmds, tickCmd())
		}

		return m, tea.Batch(cmds...)

	case portCheckMsg:
		var cmds []tea.Cmd
		if proc, ok := m.processes[msg.idx]; ok {
			project := m.projects[msg.idx]
			if msg.isUp {
				if proc.Status == StatusStarting || proc.Status == StatusFailed {
					proc.Status = StatusRunning
					proc.PortCheckRetries = 0
					delete(m.errorMessages, msg.idx)
				}
			} else {
				// Port is not up yet
				if proc.Status == StatusStarting {
					// For starting processes, check again soon (200ms)
					cmds = append(cmds, tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
						return checkPortCmd(msg.idx, project.Port)()
					}))
				} else if proc.ProcessDone && proc.Status == StatusStarting {
					// Process exited but port never came up
					proc.PortCheckRetries++
					if proc.PortCheckRetries >= 30 {
						proc.Status = StatusFailed
						if proc.ExitCode != 0 {
							m.errorMessages[msg.idx] = fmt.Sprintf("Process exited with code %d", proc.ExitCode)
						} else {
							m.errorMessages[msg.idx] = fmt.Sprintf("Process exited but port %d never came up", project.Port)
						}
					} else {
						// Check again after a short delay
						cmds = append(cmds, tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
							return checkPortCmd(msg.idx, project.Port)()
						}))
					}
				}
			}
		}
		if len(cmds) > 0 {
			return m, tea.Batch(cmds...)
		}
		return m, nil

	case processExitMsg:
		var cmds []tea.Cmd
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
				// Port not up yet, schedule a check soon
				if msg.exitCode == 0 {
					// Process exited successfully, might be daemonized - check port soon
					cmds = append(cmds, tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
						return checkPortCmd(msg.idx, project.Port)()
					}))
				} else {
					// Process exited with error
					proc.Status = StatusFailed
					m.errorMessages[msg.idx] = fmt.Sprintf("Process exited with code %d", msg.exitCode)
				}
			}
		}
		if len(cmds) > 0 {
			return m, tea.Batch(cmds...)
		}
		return m, nil

	case restartProjectMsg:
		// Start the project after restart delay
		cmd := m.toggleProject(msg.idx)
		if cmd != nil {
			return m, cmd
		}
		return m, nil

	case tea.KeyMsg:
		// Handle log view mode
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

		// Handle search mode
		if m.searchMode {
			switch msg.Type {
			case tea.KeyEsc:
				// Exit search mode and clear search
				m.searchMode = false
				m.searchQuery = ""
				m.updateFilter()
				return m, nil
			case tea.KeyEnter:
				// Exit search mode but keep filter
				m.searchMode = false
				return m, nil
			case tea.KeyBackspace:
				if len(m.searchQuery) > 0 {
					m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
					m.updateFilter()
				}
				return m, nil
			case tea.KeyRunes:
				m.searchQuery += string(msg.Runes)
				m.updateFilter()
				return m, nil
			case tea.KeySpace:
				m.searchQuery += " "
				m.updateFilter()
				return m, nil
			}
			return m, nil
		}

		// Normal mode
		switch {
		case msg.String() == "/":
			// Enter search mode
			m.searchMode = true
			return m, nil

		case key.Matches(msg, m.keys.Quit):
			// If there's a search filter, clear it first
			if m.searchQuery != "" {
				m.searchQuery = ""
				m.updateFilter()
				return m, nil
			}
			m.killAll()
			m.quitting = true
			return m, tea.Quit

		case key.Matches(msg, m.keys.Up):
			if m.cursor > 0 {
				m.cursor--
			}

		case key.Matches(msg, m.keys.Down):
			if m.cursor < len(m.filteredIdxs)-1 {
				m.cursor++
			}

		case key.Matches(msg, m.keys.Toggle):
			projectIdx := m.getSelectedProjectIndex()
			if projectIdx >= 0 {
				cmd := m.toggleProject(projectIdx)
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
			return m, tea.Batch(cmds...)

		case key.Matches(msg, m.keys.ToggleLogs):
			projectIdx := m.getSelectedProjectIndex()
			if projectIdx >= 0 {
				if proc, ok := m.processes[projectIdx]; ok {
					m.showLogs = true
					content := proc.Logs.GetContent()
					m.viewport.SetContent(content)
					m.viewport.GotoBottom()
				}
			}

		case key.Matches(msg, m.keys.Restart):
			// Restart the selected project
			projectIdx := m.getSelectedProjectIndex()
			if projectIdx >= 0 {
				// If running, stop it first
				if proc, ok := m.processes[projectIdx]; ok {
					project := m.projects[projectIdx]
					if proc.Cmd.Process != nil {
						syscall.Kill(-proc.PID, syscall.SIGTERM)
					}
					// Also kill anything using the port (e.g., Docker containers)
					if project.Port > 0 {
						killProcessOnPort(project.Port)
					}
					delete(m.processes, projectIdx)
					delete(m.errorMessages, projectIdx)
				}
				// Wait a moment for cleanup, then start
				return m, tea.Tick(300*time.Millisecond, func(t time.Time) tea.Msg {
					return restartProjectMsg{idx: projectIdx}
				})
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
		StartTime:        time.Now(),
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

	// ═══════════════════════════════════════════════════════════════════════════
	// HEADER
	// ═══════════════════════════════════════════════════════════════════════════
	header := headerStyle.Render("DOCEYE")
	header += "  " + versionStyle.Render("process manager")

	// Status summary in header
	starting, running, failed := m.countByStatus()
	total := len(m.projects)
	header += "  " + dimStyle.Render("│")
	header += fmt.Sprintf("  %s/%d", successText.Render(fmt.Sprintf("%d", running)), total)

	if starting > 0 {
		header += "  " + warningText.Render(fmt.Sprintf("~%d", starting))
	}
	if failed > 0 {
		header += "  " + errorText.Render(fmt.Sprintf("!%d", failed))
	}

	if m.configReloaded {
		header += "  " + warningText.Render("[RELOADED]")
	}

	b.WriteString(header)
	b.WriteString("\n")
	b.WriteString(dimStyle.Render(strings.Repeat("─", 80)))
	b.WriteString("\n\n")

	// ═══════════════════════════════════════════════════════════════════════════
	// SEARCH BAR
	// ═══════════════════════════════════════════════════════════════════════════
	if m.searchMode {
		searchLine := searchPromptStyle.Render("FILTER: ")
		if m.searchQuery == "" {
			searchLine += searchPlaceholderStyle.Render("type to search name, path, or command...")
		} else {
			searchLine += searchQueryStyle.Render(m.searchQuery) + accentStyle.Render("█")
		}
		searchLine += "  " + secondaryText.Render(fmt.Sprintf("(%d/%d)", len(m.filteredIdxs), len(m.projects)))
		b.WriteString(searchBoxStyle.Render(searchLine))
		b.WriteString("\n\n")
	} else if m.searchQuery != "" {
		// Show active filter
		filterLine := accentStyle.Render("FILTER: ") + primaryText.Render(m.searchQuery)
		filterLine += "  " + secondaryText.Render(fmt.Sprintf("(%d/%d)", len(m.filteredIdxs), len(m.projects)))
		filterLine += "  " + helpDescStyle.Render("[q to clear]")
		b.WriteString(filterLine)
		b.WriteString("\n\n")
	}

	// ═══════════════════════════════════════════════════════════════════════════
	// PROJECT LIST
	// ═══════════════════════════════════════════════════════════════════════════
	if len(m.filteredIdxs) == 0 {
		b.WriteString(secondaryText.Render("  No projects match filter\n"))
	}

	for cursorPos, projectIdx := range m.filteredIdxs {
		project := m.projects[projectIdx]
		isSelected := cursorPos == m.cursor
		status := m.getStatus(projectIdx)
		proc := m.processes[projectIdx]

		// ─────────────────────────────────────────────────────────────────────
		// Project separator (between projects, not before first)
		// ─────────────────────────────────────────────────────────────────────
		if cursorPos > 0 {
			b.WriteString(dimStyle.Render("  ·─────────────────────────────────────────────────────────────────────────"))
			b.WriteString("\n")
		}

		// ─────────────────────────────────────────────────────────────────────
		// Main project row
		// ─────────────────────────────────────────────────────────────────────
		// Cursor indicator
		if isSelected {
			b.WriteString(highlightText.Render("▸ "))
		} else {
			b.WriteString("  ")
		}

		// Status icon
		switch status {
		case StatusRunning:
			b.WriteString(successText.Render("● "))
		case StatusStarting:
			frames := []string{"◐", "◓", "◑", "◒"}
			b.WriteString(warningText.Render(frames[m.spinner%len(frames)] + " "))
		case StatusFailed:
			b.WriteString(errorText.Render("✖ "))
		default:
			b.WriteString(secondaryText.Render("○ "))
		}

		// Project name (padded to 18 chars)
		name := truncateString(project.Name, 18)
		namePadded := fmt.Sprintf("%-18s", name)
		if isSelected {
			b.WriteString(primaryText.Bold(true).Render(namePadded))
		} else if status == StatusStopped {
			b.WriteString(secondaryText.Render(namePadded))
		} else {
			b.WriteString(primaryText.Render(namePadded))
		}

		b.WriteString("  ")

		// Status text (padded to 10 chars)
		var statusStr string
		switch status {
		case StatusRunning:
			statusStr = successText.Render(fmt.Sprintf("%-10s", "RUNNING"))
		case StatusStarting:
			statusStr = warningText.Render(fmt.Sprintf("%-10s", "STARTING"))
		case StatusFailed:
			statusStr = errorText.Render(fmt.Sprintf("%-10s", "FAILED"))
		default:
			statusStr = secondaryText.Render(fmt.Sprintf("%-10s", "STOPPED"))
		}
		b.WriteString(statusStr)

		b.WriteString("  ")

		// PID (padded to 8 chars)
		var pidStr string
		if proc != nil && status != StatusStopped {
			if status == StatusFailed {
				pidStr = errorText.Render(fmt.Sprintf("%-8s", fmt.Sprintf("x:%d", proc.ExitCode)))
			} else {
				pidStr = primaryText.Render(fmt.Sprintf("%-8d", proc.PID))
			}
		} else {
			pidStr = secondaryText.Render(fmt.Sprintf("%-8s", "---"))
		}
		b.WriteString(pidStr)

		b.WriteString("  ")

		// Uptime (padded to 8 chars)
		var uptimeStr string
		if proc != nil && (status == StatusRunning || status == StatusStarting) {
			uptimeStr = successText.Render(fmt.Sprintf("%-8s", formatUptime(time.Since(proc.StartTime))))
		} else {
			uptimeStr = secondaryText.Render(fmt.Sprintf("%-8s", "---"))
		}
		b.WriteString(uptimeStr)

		b.WriteString("  ")

		// Port
		if project.Port > 0 {
			if status == StatusRunning {
				b.WriteString(highlightText.Render(fmt.Sprintf(":%d", project.Port)))
			} else {
				b.WriteString(secondaryText.Render(fmt.Sprintf(":%d", project.Port)))
			}
		}

		b.WriteString("\n")

		// ─────────────────────────────────────────────────────────────────────
		// Expanded Details (only for selected project)
		// ─────────────────────────────────────────────────────────────────────
		if isSelected {
			// Path
			b.WriteString(dimStyle.Render("    │ "))
			b.WriteString(secondaryText.Render("path  "))
			b.WriteString(valueStyle.Render(project.Path))
			b.WriteString("\n")

			// Command
			b.WriteString(dimStyle.Render("    │ "))
			b.WriteString(secondaryText.Render("cmd   "))
			cmdDisplay := truncateString(project.Command, 55)
			b.WriteString(valueStyle.Render(cmdDisplay))
			b.WriteString("\n")

			// URL
			if project.Port > 0 {
				b.WriteString(dimStyle.Render("    │ "))
				b.WriteString(secondaryText.Render("url   "))
				b.WriteString(linkText.Render(project.URL()))
				b.WriteString("\n")
			}

			// Log preview
			if proc != nil {
				lines := proc.Logs.GetLines()
				logCount := len(lines)
				b.WriteString(dimStyle.Render("    │ "))
				b.WriteString(secondaryText.Render("logs  "))
				b.WriteString(secondaryText.Render(fmt.Sprintf("%d lines", logCount)))
				if logCount > 0 {
					lastLine := truncateString(lines[logCount-1], 45)
					b.WriteString(dimStyle.Render(" ╴ "))
					b.WriteString(mutedText.Render(lastLine))
				}
				b.WriteString("\n")
			}

			// Error message
			if errMsg, ok := m.errorMessages[projectIdx]; ok {
				b.WriteString(dimStyle.Render("    │ "))
				b.WriteString(errorText.Bold(true).Render("err   "))
				b.WriteString(errorText.Render(errMsg))
				b.WriteString("\n")
			}

			b.WriteString(dimStyle.Render("    ╰"))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")

	// ═══════════════════════════════════════════════════════════════════════════
	// LOGS PANEL
	// ═══════════════════════════════════════════════════════════════════════════
	if m.showLogs {
		projectIdx := m.getSelectedProjectIndex()
		if projectIdx >= 0 {
			if proc, ok := m.processes[projectIdx]; ok {
				project := m.projects[projectIdx]

				// Log header
				logHeader := panelTitleStyle.Render("LOGS")
				logHeader += "  " + primaryText.Render(project.Name)
				logHeader += "  " + secondaryText.Render(fmt.Sprintf("pid:%d", proc.PID))
				logHeader += "  " + secondaryText.Render(fmt.Sprintf("%d lines", len(proc.Logs.GetLines())))
				if proc.Status == StatusFailed {
					logHeader += "  " + errorText.Render(fmt.Sprintf("[exit:%d]", proc.ExitCode))
				}

				logContent := logHeader + "\n" + dimStyle.Render(strings.Repeat("─", 76)) + "\n" + m.viewport.View()
				b.WriteString(panelStyle.Render(logContent))
				b.WriteString("\n")
			}
		}
	}

	// ═══════════════════════════════════════════════════════════════════════════
	// STATUS BAR
	// ═══════════════════════════════════════════════════════════════════════════
	var statusParts []string
	if running > 0 {
		statusParts = append(statusParts, badgeRunning.Render(fmt.Sprintf(" %d RUN ", running)))
	}
	if starting > 0 {
		statusParts = append(statusParts, badgeStarting.Render(fmt.Sprintf(" %d WAIT ", starting)))
	}
	if failed > 0 {
		statusParts = append(statusParts, badgeFailed.Render(fmt.Sprintf(" %d FAIL ", failed)))
	}
	stopped := total - running - starting - failed
	if stopped > 0 {
		statusParts = append(statusParts, badgeStopped.Render(fmt.Sprintf(" %d STOP ", stopped)))
	}

	if len(statusParts) > 0 {
		b.WriteString(strings.Join(statusParts, " "))
	}
	b.WriteString("\n\n")

	// ═══════════════════════════════════════════════════════════════════════════
	// HELP BAR
	// ═══════════════════════════════════════════════════════════════════════════
	var helpItems []string
	if m.searchMode {
		helpItems = []string{
			helpDescStyle.Render("Type to filter"),
			helpKeyStyle.Render("Enter") + helpDescStyle.Render(" apply"),
			helpKeyStyle.Render("Esc") + helpDescStyle.Render(" cancel"),
		}
	} else if m.showLogs {
		helpItems = []string{
			helpKeyStyle.Render("j/k") + helpDescStyle.Render(" scroll"),
			helpKeyStyle.Render("u/d") + helpDescStyle.Render(" page"),
			helpKeyStyle.Render("Esc") + helpDescStyle.Render(" close"),
		}
	} else {
		helpItems = []string{
			helpKeyStyle.Render("/") + helpDescStyle.Render(" filter"),
			helpKeyStyle.Render("j/k") + helpDescStyle.Render(" select"),
			helpKeyStyle.Render("Enter") + helpDescStyle.Render(" start/stop"),
			helpKeyStyle.Render("l") + helpDescStyle.Render(" logs"),
			helpKeyStyle.Render("r") + helpDescStyle.Render(" restart"),
			helpKeyStyle.Render("q") + helpDescStyle.Render(" quit"),
		}
	}
	b.WriteString(strings.Join(helpItems, helpSepStyle.Render("  ")))

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
