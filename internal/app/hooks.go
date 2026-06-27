package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

// HookSpec is a lifecycle hook (config.toml [[hooks]]) that enforces team
// procedure. Events:
//
//	PreToolUse  — before a tool runs; non-zero exit BLOCKS the tool (output → agent)
//	PostToolUse — after a file-modifying tool; non-zero exit feeds output back
//	Stop        — before a turn finishes; non-zero exit forces the agent to continue
//
// The command runs via the shell with a JSON payload on stdin and key fields in
// env (MAGI_TOOL, MAGI_PATH). Match filters by tool name ("*"=any).
type HookSpec struct {
	Event   string
	Match   string
	Command string
}

func (h HookSpec) matches(event, tool string) bool {
	if !strings.EqualFold(h.Event, event) {
		return false
	}
	return h.Match == "" || h.Match == "*" || h.Match == tool
}

// runHook executes one hook command, returning (output, blocked).
func (a *App) runHook(ctx context.Context, h HookSpec, workdir, tool, path string) (string, bool) {
	if a.plat == nil {
		return "", false
	}
	payload, _ := json.Marshal(map[string]string{"event": h.Event, "tool": tool, "path": path, "workdir": workdir})
	hctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	res, err := a.plat.Exec(hctx, port.Cmd{
		Path:  "/bin/sh",
		Args:  []string{"-c", h.Command},
		Dir:   workdir,
		Env:   []string{"MAGI_TOOL=" + tool, "MAGI_PATH=" + path},
		Stdin: payload,
	})
	if err != nil {
		return "", false
	}
	if res.ExitCode != 0 {
		out := strings.TrimSpace(string(res.Stderr))
		if out == "" {
			out = strings.TrimSpace(string(res.Stdout))
		}
		return out, true
	}
	return "", false
}

// runPreToolHooks returns blocking feedback (non-empty = block the tool).
func (a *App) runPreToolHooks(ctx context.Context, workdir, tool, path string) string {
	var fb []string
	for _, h := range a.cfg.Hooks {
		if !h.matches("PreToolUse", tool) {
			continue
		}
		if out, blocked := a.runHook(ctx, h, workdir, tool, path); blocked {
			fb = append(fb, out)
		}
	}
	return strings.Join(fb, "\n")
}

// runPostToolHooks runs the built-in harness action (auto-format on edit) plus
// configured PostToolUse hooks, returning combined feedback (empty if clean).
func (a *App) runPostToolHooks(ctx context.Context, workdir, tool, path string) string {
	// Built-in harness: auto-format Go files on save.
	if a.cfg.Harness && fileModifiers[tool] && filepath.Ext(path) == ".go" && a.plat != nil {
		_, _ = a.plat.Exec(ctx, port.Cmd{Path: "gofmt", Args: []string{"-w", path}, Dir: workdir})
	}
	var fb []string
	for _, h := range a.cfg.Hooks {
		if !h.matches("PostToolUse", tool) {
			continue
		}
		if out, blocked := a.runHook(ctx, h, workdir, tool, path); blocked && out != "" {
			fb = append(fb, "[hook] "+out)
		}
	}
	return strings.Join(fb, "\n")
}

// runStopHooks runs Stop hooks before a turn finishes; non-empty result means a
// check failed and the agent should keep working.
func (a *App) runStopHooks(ctx context.Context, workdir string) string {
	var fb []string
	for _, h := range a.cfg.Hooks {
		if !h.matches("Stop", "") {
			continue
		}
		if out, blocked := a.runHook(ctx, h, workdir, "", ""); blocked && out != "" {
			fb = append(fb, out)
		}
	}
	return strings.Join(fb, "\n")
}
