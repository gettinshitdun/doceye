package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Project represents a single project configuration
type Project struct {
	Name    string `yaml:"name"`
	Path    string `yaml:"path"`
	Command string `yaml:"command"`
	Port    int    `yaml:"port,omitempty"`
}

// Config represents the root configuration
type Config struct {
	Projects []Project `yaml:"projects"`
}

// URL returns the URL for projects with a port
func (p *Project) URL() string {
	if p.Port == 0 {
		return ""
	}
	return fmt.Sprintf("http://localhost:%d", p.Port)
}

// DefaultConfigPath returns the default config file path
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".doceye.yaml"
	}
	return filepath.Join(home, ".doceye.yaml")
}

// Load reads and parses the config file from the given path
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if len(cfg.Projects) == 0 {
		return nil, fmt.Errorf("no projects found in config file")
	}

	return &cfg, nil
}

// CreateSampleConfig creates a sample config file at the given path
func CreateSampleConfig(path string) error {
	sample := Config{
		Projects: []Project{
			{
				Name:    "Example API",
				Path:    "/path/to/your/api",
				Command: "go run main.go",
				Port:    8080,
			},
			{
				Name:    "Example Frontend",
				Path:    "/path/to/your/frontend",
				Command: "npm run dev",
				Port:    3000,
			},
			{
				Name:    "Background Worker",
				Path:    "/path/to/worker",
				Command: "python worker.py",
			},
		},
	}

	data, err := yaml.Marshal(&sample)
	if err != nil {
		return fmt.Errorf("failed to marshal sample config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write sample config: %w", err)
	}

	return nil
}
