// Package platform implements port.Platform, abstracting OS-specific behavior
// (exec, config/data dirs, terminal capability detection) so the core stays
// OS-agnostic. Pure Go, no CGo — preserves cross-compilation (§9.5).
package platform

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sayaya1090/magi/internal/port"
)

// OS implements port.Platform for the host operating system.
type OS struct{}

// New returns a host platform adapter.
func New() *OS { return &OS{} }

// Exec runs a command and captures its output.
func (OS) Exec(ctx context.Context, c port.Cmd) (port.ExecResult, error) {
	cmd := exec.CommandContext(ctx, c.Path, c.Args...)
	cmd.Dir = c.Dir
	if len(c.Env) > 0 {
		cmd.Env = append(os.Environ(), c.Env...)
	}
	if len(c.Stdin) > 0 {
		cmd.Stdin = strings.NewReader(string(c.Stdin))
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res := port.ExecResult{
		Stdout: []byte(stdout.String()),
		Stderr: []byte(stderr.String()),
	}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			// Non-zero exit is reported via ExitCode, not as a Go error.
			return res, nil
		}
		return res, err
	}
	return res, nil
}

// ConfigDir returns the magi config directory (e.g. ~/.config/magi).
func (OS) ConfigDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "magi")
	}
	return filepath.Join(base, "magi")
}

// DataDir returns the magi data directory (e.g. ~/.cache/magi on Linux,
// ~/Library/Caches/magi on macOS, %LocalAppData%/magi on Windows).
func (OS) DataDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "magi")
	}
	return filepath.Join(base, "magi")
}

// TerminalCaps detects truecolor support and the best inline-image protocol
// (D8). M1 performs env-based detection only; DA1 querying is added in M2.
func (OS) TerminalCaps() port.TermCaps {
	caps := port.TermCaps{}

	if ct := os.Getenv("COLORTERM"); ct == "truecolor" || ct == "24bit" {
		caps.TrueColor = true
	}
	term := os.Getenv("TERM")
	termProgram := os.Getenv("TERM_PROGRAM")

	switch {
	case term == "xterm-kitty", os.Getenv("KITTY_WINDOW_ID") != "":
		caps.Image = "kitty"
		caps.TrueColor = true
	case termProgram == "iTerm.app":
		caps.Image = "iterm2"
		caps.TrueColor = true
	case termProgram == "WezTerm":
		caps.Image = "iterm2"
		caps.TrueColor = true
	case strings.Contains(term, "256color") && caps.TrueColor:
		// truecolor already set; no image protocol known → half-block fallback
	}
	return caps
}
