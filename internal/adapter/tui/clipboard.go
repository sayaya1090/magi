package tui

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// copyToOSClipboard writes text to the operating-system clipboard via the
// platform's native tool (pbcopy/wl-copy/xclip/xsel/clip). Best-effort: returns
// false if no tool is available. This complements tea.SetClipboard (OSC52),
// which some terminals (e.g. macOS Terminal.app) ignore.
func copyToOSClipboard(text string) bool {
	name, args := clipboardCmd()
	if name == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run() == nil
}

// clipboardCmd picks an available clipboard tool for the OS.
func clipboardCmd() (string, []string) {
	switch runtime.GOOS {
	case "darwin":
		return "pbcopy", nil
	case "windows":
		return "clip", nil
	default: // linux/bsd: prefer Wayland, then X11
		for _, c := range [][]string{
			{"wl-copy"},
			{"xclip", "-selection", "clipboard"},
			{"xsel", "--clipboard", "--input"},
		} {
			if _, err := exec.LookPath(c[0]); err == nil {
				return c[0], c[1:]
			}
		}
	}
	return "", nil
}
