# doceye

A TUI project launcher built with [Bubble Tea](https://github.com/charmbracelet/bubbletea).

## Features

- Configure multiple projects in a YAML file
- Beautiful terminal UI with keyboard navigation
- Quick project launching with a single keystroke
- Runs commands in the background

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
  - name: "My API Server"
    path: "/path/to/api"
    command: "go run main.go"
    port: 8080

  - name: "Frontend Dev"
    path: "/path/to/frontend"
    command: "npm run dev"
    port: 3000

  - name: "Background Worker"
    path: "/path/to/worker"
    command: "python worker.py"
```

### 3. Launch the TUI

```bash
doceye
```

### Custom config path

```bash
doceye --config /path/to/custom-config.yaml
```

## Config Options

| Option | Required | Description |
|--------|----------|-------------|
| `name` | Yes | Project display name |
| `path` | Yes | Project directory path |
| `command` | Yes | Command to run |
| `port` | No | Port number (shows URL in output) |

## Keybindings

| Key | Action |
|-----|--------|
| `k` / `up` | Move cursor up |
| `j` / `down` | Move cursor down |
| `Enter` | Select and run project |
| `q` / `Esc` | Quit |

## CLI Flags

| Flag | Description |
|------|-------------|
| `--init` | Create a sample config file |
| `--config <path>` | Use a custom config file path |

## License

MIT
