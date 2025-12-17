package main

import (
	"flag"
	"fmt"
	"os"

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
		fmt.Println("   port:    Port number (optional, shows URL)")
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
	if _, err := tui.Run(cfg.Projects, *configPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
