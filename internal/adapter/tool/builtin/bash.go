package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Bash runs a shell command in the working directory and returns its combined
// output and exit code, with a timeout. It is a "danger" tool (permission-gated)
// and the agent's escape hatch for builds/tests/git. (F-TOOL bash)
type Bash struct{}

type bashArgs struct {
	Command    string `json:"command"`
	Timeout    int    `json:"timeout"`    // seconds (default 120, max 600)
	Background bool   `json:"background"` // run detached; returns an id to poll/kill
}

func (Bash) Name() string { return "bash" }
func (Bash) Description() string {
	return "Run a shell command in the working directory. Returns combined stdout/stderr and the exit code. Use for builds, tests, git, and file operations not covered by other tools. Set background=true for a long-running command (dev server, watcher, slow build): it returns an id immediately; read its output with bash_output and stop it with bash_kill."
}
func (Bash) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"},"timeout":{"type":"integer","description":"seconds (default 120, max 600)"},"background":{"type":"boolean","description":"run detached; returns an id for bash_output/bash_kill"}},"required":["command"]}`)
}

func (Bash) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var a bashArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	if strings.TrimSpace(a.Command) == "" {
		return errResult("", "command is required"), nil
	}
	if a.Background {
		p, err := bg.start(env.Workdir, env.Sandbox, a.Command)
		if err != nil {
			return errResult("", "failed to start background command: "+err.Error()), nil
		}
		return okText("", fmt.Sprintf("started background command %s — poll with bash_output{id:%q}, stop with bash_kill{id:%q}", p.id, p.id, p.id)), nil
	}
	timeout := a.Timeout
	if timeout <= 0 {
		timeout = 120
	}
	if timeout > 600 {
		timeout = 600
	}

	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	name, args := shell(a.Command)
	// OS-level confinement (sandbox axis): wrap the command so writes
	// stay in the workspace and the network is off, when the platform supports it.
	// Falls back to unconfined transparently — the policy layer's command scan and
	// permission prompt still apply either way.
	if argv, wrapped := sandboxArgv(env.Sandbox, a.Command); wrapped {
		name, args = argv[0], argv[1:]
	}
	cmd := exec.CommandContext(cctx, name, args...)
	cmd.Dir = env.Workdir
	// On timeout, CommandContext kills the shell but a child (e.g. `sleep`) can
	// keep the output pipe open, blocking CombinedOutput until the child exits.
	// WaitDelay bounds that post-kill wait so a timed-out command returns promptly.
	cmd.WaitDelay = 2 * time.Second
	// Windows confinement is applied as a process token (no CLI wrapper exists).
	cmd.SysProcAttr = sandboxProcAttr(env.Sandbox)
	out, err := cmd.CombinedOutput()
	// Safety net: if a sandboxed launch fails to START (process never ran), retry
	// unconfined so confinement can never break the bash tool outright.
	if err != nil && cmd.ProcessState == nil && cmd.SysProcAttr != nil {
		cmd = exec.CommandContext(cctx, name, args...)
		cmd.Dir = env.Workdir
		out, err = cmd.CombinedOutput()
	}

	exit := 0
	if cmd.ProcessState != nil {
		exit = cmd.ProcessState.ExitCode()
	}
	body := string(out)
	if cctx.Err() == context.DeadlineExceeded {
		body += fmt.Sprintf("\n[timed out after %ds]", timeout)
		return errResult("", truncateOut(body)), nil
	}
	if err != nil && exit == 0 {
		// Launch failure (command not found, etc.) rather than non-zero exit.
		return errResult("", err.Error()+"\n"+truncateOut(body)), nil
	}
	res := okText("", fmt.Sprintf("exit %d\n%s", exit, truncateOut(body)))
	res.IsError = exit != 0
	return res, nil
}

// shell returns the platform shell invocation for a command string.
func shell(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "powershell", []string{"-NoProfile", "-Command", command}
	}
	return "/bin/sh", []string{"-c", command}
}

// truncateOut caps very large command output.
func truncateOut(s string) string {
	const max = 30 * 1024
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n…(output truncated)"
}
