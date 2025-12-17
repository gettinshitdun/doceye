package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/sujal/doceye/config"
	"github.com/sujal/doceye/tui"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", config.DefaultConfigPath(), "path to config file")
	initConfig := flag.Bool("init", false, "create a sample config file")
	flag.Parse()

	// Handle --init flag
	if *initConfig {
		if err := config.CreateSampleConfig(*configPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating sample config: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("[OK] Created sample config at: %s\n", *configPath)
		fmt.Println("Edit this file to add your projects!")
		fmt.Println("")
		fmt.Println("Config options:")
		fmt.Println("   name:    Project name")
		fmt.Println("   path:    Project directory")
		fmt.Println("   command: Command to run")
		fmt.Println("   port:    Port number (optional, for browser access)")
		return
	}

	// Load config
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		fmt.Fprintf(os.Stderr, "\nRun 'doceye --init' to create a sample config file.\n")
		os.Exit(1)
	}

	// Run the TUI
	selected, err := tui.Run(cfg.Projects)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// If user quit without selecting, exit gracefully
	if selected == nil {
		return
	}

	// Run the selected project
	runProject(selected)
}

func runProject(project *config.Project) {
	fmt.Printf("\nLaunching: %s\n", project.Name)
	fmt.Printf("Directory: %s\n", project.Path)
	fmt.Printf("Command: %s\n", project.Command)

	if project.Port > 0 {
		fmt.Printf("URL: %s\n", project.URL())
	}

	// Check if directory exists
	if _, err := os.Stat(project.Path); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: directory does not exist: %s\n", project.Path)
		os.Exit(1)
	}

	// Get shell from environment or use default
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}

	// Create the command using shell
	cmd := exec.Command(shell, "-c", project.Command)
	cmd.Dir = project.Path

	// Detach the process from the parent
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	// Start the command in the background
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting command: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("[OK] Started in background (PID: %d)\n", cmd.Process.Pid)
	fmt.Println("Tip: Use 'kill " + fmt.Sprint(cmd.Process.Pid) + "' to stop it")
}
