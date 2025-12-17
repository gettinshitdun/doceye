# doceye

An interactive TUI project launcher built with [Bubble Tea](https://github.com/charmbracelet/bubbletea).

## Features

- Configure multiple projects in a YAML file
- Interactive terminal UI with keyboard navigation
- Start and stop projects with a single keystroke
- Run multiple projects simultaneously
- View real-time logs for running projects
- All processes are killed when you quit

## Installation

```bash
go install github.com/sujal/doceye@latest
```

Or build from source:

```bash
git clone https://github.com/sujal/doceye.git
cd doceye
make install
```

## Usage

### 1. Create a config file

```bash
doceye --init
```

This creates a sample config at `~/.doceye.yaml`.

### 2. Edit the config file

```yaml
projects:
  - name: "API Server"
    path: "/path/to/api"
    command: "go run main.go"
    port: 8080

  - name: "Frontend"
    path: "/path/to/frontend"
    command: "npm run dev"
    port: 3000

  - name: "Worker"
    path: "/path/to/worker"
    command: "python worker.py"
```

### 3. Launch the TUI

```bash
doceye
```

## Config Options

| Option | Required | Description |
|--------|----------|-------------|
| `name` | Yes | Project display name |
| `path` | Yes | Project directory path |
| `command` | Yes | Command to run |
| `port` | No | Port number (shows URL, enables STARTING status) |

## Keybindings

### Main View

| Key | Action |
|-----|--------|
| `j` / `down` | Move cursor down |
| `k` / `up` | Move cursor up |
| `enter` / `space` | Toggle project (start/stop) |
| `l` | View logs for selected project |
| `q` / `ctrl+c` | Quit (kills all running projects) |

### Log View

| Key | Action |
|-----|--------|
| `j` / `down` | Scroll down |
| `k` / `up` | Scroll up |
| `ctrl+d` | Page down |
| `ctrl+u` | Page up |
| `l` / `q` / `esc` | Close log view |

## CLI Flags

| Flag | Description |
|------|-------------|
| `--init` | Create a sample config file |
| `--config <path>` | Use a custom config file path |

## Status Indicators

| Status | Description |
|--------|-------------|
| `[--]` | Stopped |
| `[.]` `[..]` `[...]` | Starting (waiting for port) |
| `[ON]` | Running |

## License

MIT
