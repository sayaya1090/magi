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
	// Detach the controlling terminal so an interactive command (e.g. `!ssh host`
	// prompting for a password) can't open /dev/tty and seize the TUI's terminal,
	// which corrupts the display. stdin stays /dev/null when unset, so tty-less
	// prompts fail fast rather than hang. No-op on Windows.
	detach(cmd)
	stdout := &capWriter{limit: c.MaxOutput}
	stderr := &capWriter{limit: c.MaxOutput}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	res := port.ExecResult{
		Stdout: stdout.buf,
		Stderr: stderr.buf,
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

// capWriter accumulates written bytes up to limit (0 = unlimited), silently
// discarding the overflow while still reporting a full write so the child process
// is never blocked by a full pipe — bounds capture memory for chatty commands.
type capWriter struct {
	buf   []byte
	limit int
}

func (w *capWriter) Write(p []byte) (int, error) {
	if w.limit <= 0 {
		w.buf = append(w.buf, p...)
		return len(p), nil
	}
	if room := w.limit - len(w.buf); room > 0 {
		if room > len(p) {
			room = len(p)
		}
		w.buf = append(w.buf, p[:room]...)
	}
	return len(p), nil
}

// ConfigDir returns the magi config directory (e.g. ~/.config/magi).
//
// MAGI_CONFIG_DIR overrides it outright, so two instances on one machine can be
// given separate config trees (their own config.toml, plugin auth store, etc.).
// Without it both share ~/.config/magi/config.toml, and a plugin that persists a
// runtime choice (a live set_model, an SSO login) has one instance's write land
// in the other's file — the multi-instance collision point.
func (OS) ConfigDir() string {
	if d := strings.TrimSpace(os.Getenv("MAGI_CONFIG_DIR")); d != "" {
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "magi")
	}
	return filepath.Join(base, "magi")
}

// DataDir returns the magi data directory (e.g. ~/.cache/magi on Linux,
// ~/Library/Caches/magi on macOS, %LocalAppData%/magi on Windows).
//
// MAGI_DATA_DIR overrides it, the DataDir counterpart of MAGI_CONFIG_DIR — it
// isolates the plugin data store (<data>/plugin-data/<name>.json, where an SSO
// plugin caches its token) so parallel instances don't share one token slot.
func (OS) DataDir() string {
	if d := strings.TrimSpace(os.Getenv("MAGI_DATA_DIR")); d != "" {
		return d
	}
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
