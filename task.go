package main

import (
	"bufio"
	"bytes"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// task is a long-running brew command whose output streams into a modal.
type task struct {
	id      int
	title   string
	args    []string
	lines   []string
	running bool
	err     error
	started time.Time
	ended   time.Time
	cmd     *exec.Cmd
	ch      chan tea.Msg
	scroll  int // lines scrolled up from the bottom; 0 = follow output
	// softFail marks commands whose non-zero exit just means "warnings"
	// (e.g. brew doctor), not a failed operation.
	softFail bool
}

type taskLineMsg struct {
	id   int
	line string
}

type taskDoneMsg struct {
	id  int
	err error
}

type execDoneMsg struct{ err error }

var taskSeq int

func startTask(title string, args ...string) (*task, tea.Cmd) {
	taskSeq++
	t := &task{
		id:      taskSeq,
		title:   title,
		args:    args,
		running: true,
		started: time.Now(),
		ch:      make(chan tea.Msg, 512),
		lines:   []string{"$ brew " + strings.Join(args, " ")},
	}

	cmd := exec.Command("brew", args...)
	cmd.Env = brewEnv()
	// Run in a new session with no controlling terminal so nothing (sudo,
	// git credential prompts…) can fight Bubble Tea for the TTY. Commands that
	// truly need a terminal fail fast and can be re-run with `t`.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw
	cmd.Stdin = nil
	t.cmd = cmd

	if err := cmd.Start(); err != nil {
		t.ch <- taskDoneMsg{id: t.id, err: err}
		return t, waitTask(t.ch)
	}

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		sc.Split(scanLinesCR)
		for sc.Scan() {
			line := strings.TrimRight(ansi.Strip(sc.Text()), " \t")
			t.ch <- taskLineMsg{id: t.id, line: line}
		}
		_, _ = io.Copy(io.Discard, pr) // keep draining if the scanner gave up
	}()
	go func() {
		err := cmd.Wait()
		pw.Close()
		<-readDone
		t.ch <- taskDoneMsg{id: t.id, err: err}
	}()

	return t, waitTask(t.ch)
}

func waitTask(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

func (t *task) cancel() {
	if t.cmd != nil && t.cmd.Process != nil && t.running {
		// Negative pid signals the whole process group created by Setsid.
		_ = syscall.Kill(-t.cmd.Process.Pid, syscall.SIGINT)
	}
}

func (t *task) elapsed() time.Duration {
	if t.running {
		return time.Since(t.started)
	}
	return t.ended.Sub(t.started)
}

// runInTerminal hands the real terminal over to brew, for commands that need
// interactive input (e.g. a sudo password for some cask uninstalls).
func runInTerminal(args []string) tea.Cmd {
	script := `brew "$@"; s=$?; echo; printf '\033[2m── press enter to return to cellar ──\033[0m'; read _; exit $s`
	c := exec.Command("/bin/sh", append([]string{"-c", script, "sh"}, args...)...)
	return tea.ExecProcess(c, func(err error) tea.Msg { return execDoneMsg{err: err} })
}

// scanLinesCR splits on \n, \r\n or a bare \r (progress-bar style updates).
func scanLinesCR(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		if data[i] == '\r' && i+1 < len(data) && data[i+1] == '\n' {
			return i + 2, data[:i], nil
		}
		if data[i] == '\r' && i+1 == len(data) && !atEOF {
			return 0, nil, nil // might be \r\n split across reads
		}
		return i + 1, data[:i], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}
