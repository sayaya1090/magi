package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
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
	// Confined like a tool call, because that is what it is: a shell command this process runs
	// because a config file said to. It ran unconfined while `bash` two files away was wrapped —
	// so the strictest posture on the machine still had one path to a plain /bin/sh, and it was
	// the path that fires on EVERY tool use.
	path0, args := "/bin/sh", []string{"-c", h.Command}
	if a.cfg.Confine != nil {
		if argv, wrapped := a.cfg.Confine(port.SandboxSpec{
			Mode: a.cfg.Sandbox, Workdir: workdir, AllowNet: a.cfg.Permission == "allow",
			ReadOnly: readOnlyPaths(a.cfg),
		}, append([]string{path0}, args...)); wrapped {
			path0, args = argv[0], argv[1:]
		}
	}
	res, err := a.plat.Exec(hctx, port.Cmd{
		Path:  path0,
		Args:  args,
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
	var fb []string
	// Built-in harness: auto-format Go files on save — and SAY SO when it changed the bytes.
	// gofmt rewrites indentation silently, so a model that wrote four-space indent and then tried
	// an exact-string edit against what it wrote got "old string not present" and lost a step. A
	// `gofmt -l` first tells us whether the file will change; if so, the note points the model at
	// re-reading before it edits.
	if a.cfg.Harness && fileModifiers[tool] && filepath.Ext(path) == ".go" && a.plat != nil {
		listed, _ := a.plat.Exec(ctx, port.Cmd{Path: "gofmt", Args: []string{"-l", path}, Dir: workdir})
		_, _ = a.plat.Exec(ctx, port.Cmd{Path: "gofmt", Args: []string{"-w", path}, Dir: workdir})
		if strings.TrimSpace(string(listed.Stdout)) != "" {
			fb = append(fb, "[harness] gofmt reformatted "+path+" — its whitespace now differs from what you wrote; read it again before an exact-match edit.")
		}
	}
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

// stopRecord is what magi ITSELF observed by the time a turn wants to finish, rendered for whatever
// judges the finish. It sits beside the configured Stop hooks on purpose: both answer the same
// question at the same seam — "is there a reason not to end here" — one from the workspace's own
// procedure, one from magi's record of what it granted and what came back.
//
// It carries something no other input at this seam does: which commands magi could NOT determine the
// status of. A build piped through `| tail` reports exit 0, and every reader downstream — the
// council, the report, the agent itself — has been taking that as a clean build. Observed four times
// in one run, each time with magi's own note beside it saying the exit was the tail's.
//
// Informational by construction. It does not block: the two shapes worth blocking on are already
// handled — a turn that changed nothing is deliberately excused (noChanges tells the council to
// judge the report on its merits), and code authored but never run is caught by the exercise ledger
// before this point. Adding a third rule here without a run that shows it firing would be guessing.
func (a *App) stopRecord(ctx context.Context, sid session.SessionID) string {
	return a.observe(ctx, sid).render()
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
