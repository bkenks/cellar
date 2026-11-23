package main

import (
	"fmt"
	"os"
	"os/exec"

	tea "charm.land/bubbletea/v2"
)

// version is set at build time via -ldflags "-X main.version=…".
var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-v", "--version", "version":
			fmt.Println("cellar", version)
			return
		case "-h", "--help", "help":
			fmt.Println("cellar — a terminal UI for Homebrew. Run with no arguments; press ? inside for keys.")
			return
		}
	}
	if _, err := exec.LookPath("brew"); err != nil {
		fmt.Fprintln(os.Stderr, "cellar: Homebrew (brew) was not found in PATH — install it from https://brew.sh")
		os.Exit(1)
	}
	if _, err := tea.NewProgram(newModel()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "cellar:", err)
		os.Exit(1)
	}
}
