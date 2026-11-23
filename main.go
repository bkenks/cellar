package main

import (
	"fmt"
	"os"
	"os/exec"

	tea "charm.land/bubbletea/v2"
)

func main() {
	if _, err := exec.LookPath("brew"); err != nil {
		fmt.Fprintln(os.Stderr, "cellar: Homebrew (brew) was not found in PATH — install it from https://brew.sh")
		os.Exit(1)
	}
	if _, err := tea.NewProgram(newModel()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "cellar:", err)
		os.Exit(1)
	}
}
