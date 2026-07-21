package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// WaitFor blocks until a shell CONDITION succeeds (exit 0) or a timeout elapses,
// re-checking on a fixed interval. It is the guard-safe way to wait on an external
// readiness signal — a service/port coming up, a host finishing a reboot, a job
// completing. Unlike a shell `until … do sleep; done` loop (which either burns the
// bash timeout or, split across many bash calls, trips the no-progress stall
// watchdog into a futile recovery spawn), this is a SINGLE tool call: the loop
// guard sees one step, and it emits live progress so the wait is observable rather
// than a silent block. Shell-executing, so it is permission-gated and
// command-scanned exactly like bash. (F-TOOL wait_for)
type WaitFor struct{}

type waitForArgs struct {
	Condition string  `json:"condition"`
	Timeout   flexInt `json:"timeout"`  // seconds (default 300; max 1800); tolerant parse (flexInt)
	Interval  flexInt `json:"interval"` // seconds between checks (default 5; max 60); tolerant parse (flexInt)
}

func (WaitFor) Name() string { return "wait_for" }
func (WaitFor) Description() string {
	return "Block until a shell condition succeeds (exit 0) or a timeout elapses, re-checking on a fixed interval. Use to wait on an external readiness signal — a service/port coming up, a reboot finishing, a background job completing (e.g. condition \"nc -z localhost 5432\"). Prefer over a bash sleep/poll loop: one step (no stall), allows a longer wait, and reports live progress. The first exit-0 ends the wait."
}
func (WaitFor) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"condition":{"type":"string","description":"shell command re-run each interval; exit 0 ends the wait"},"timeout":{"type":"integer","description":"max seconds to wait (default 300, max 1800)"},"interval":{"type":"integer","description":"seconds between checks (default 5, max 60)"}},"required":["condition"]}`)
}

const (
	waitForDefaultTimeout  = 300
	waitForMaxTimeout      = 1800
	waitForDefaultInterval = 5
	waitForMaxInterval     = 60
	// waitForProbeCap bounds one readiness probe: a check should be quick, so a
	// hung condition can't eat the whole budget in a single evaluation.
	waitForProbeCap = 60
)

func (WaitFor) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var a waitForArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	if strings.TrimSpace(a.Condition) == "" {
		return errResult("", "condition is required"), nil
	}
	timeout := int(a.Timeout)
	if timeout <= 0 {
		timeout = waitForDefaultTimeout
	}
	if timeout > waitForMaxTimeout {
		timeout = waitForMaxTimeout
	}
	interval := int(a.Interval)
	if interval <= 0 {
		interval = waitForDefaultInterval
	}
	if interval > waitForMaxInterval {
		interval = waitForMaxInterval
	}

	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	start := time.Now()
	attempts := 0
	lastExit := 0
	lastOut := ""
	for {
		attempts++
		exit, out := waitForProbe(ctx, env, a.Condition, deadline)
		lastExit, lastOut = exit, out
		if exit == 0 {
			body := fmt.Sprintf("condition met after %s (%d checks): %s",
				time.Since(start).Round(time.Second), attempts, a.Condition)
			if s := strings.TrimSpace(out); s != "" {
				body += "\n" + truncateOut(out)
			}
			return okText("", body), nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			break
		}
		if env.EmitProgress != nil {
			env.EmitProgress(fmt.Sprintf("%s — check %d, %s elapsed, not met yet (exit %d)",
				a.Condition, attempts, time.Since(start).Round(time.Second), exit))
		}
		// Sleep the interval, but never past the deadline, and wake on cancellation.
		wait := time.Duration(interval) * time.Second
		if rem := time.Until(deadline); rem < wait {
			wait = rem
		}
		if wait <= 0 {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
		if ctx.Err() != nil {
			break
		}
	}

	elapsed := time.Since(start).Round(time.Second)
	reason := fmt.Sprintf("condition not met after %s (%d checks): %s\n[last exit %d]",
		elapsed, attempts, a.Condition, lastExit)
	if ctx.Err() != nil {
		reason = fmt.Sprintf("wait cancelled after %s (%d checks): %s", elapsed, attempts, a.Condition)
	}
	if s := strings.TrimSpace(lastOut); s != "" {
		reason += "\n" + truncateOut(lastOut)
	}
	return errResult("", reason), nil
}

// waitForProbe runs the condition once, bounded by the smaller of a short per-probe
// cap and the time remaining to the deadline, and returns its exit code and combined
// output. A command that never launches (bad condition) reports a non-zero exit so
// the wait keeps polling rather than mistaking a launch failure for success. Mirrors
// bash's confinement + tty-detachment (and its unconfined start-failure fallback).
func waitForProbe(ctx context.Context, env port.ToolEnv, condition string, deadline time.Time) (int, string) {
	probeTimeout := waitForProbeCap * time.Second
	if rem := time.Until(deadline); rem > 0 && rem < probeTimeout {
		probeTimeout = rem
	}
	if probeTimeout <= 0 {
		return 1, ""
	}
	cctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	name, args := shell(condition)
	if argv, wrapped := sandboxArgv(env.Sandbox, condition); wrapped {
		name, args = argv[0], argv[1:]
	}
	cmd := exec.CommandContext(cctx, name, args...)
	cmd.Dir = env.Workdir
	cmd.WaitDelay = 2 * time.Second
	sboxAttr := sandboxProcAttr(env.Sandbox)
	cmd.SysProcAttr = detachTTY(sboxAttr)
	out, err := runCapture(cmd)
	// Token-confined launch (Windows) that never started: retry unconfined so
	// confinement can't turn every probe into a false negative.
	if err != nil && cmd.ProcessState == nil && sboxAttr != nil {
		cmd = exec.CommandContext(cctx, name, args...)
		cmd.Dir = env.Workdir
		cmd.WaitDelay = 2 * time.Second
		cmd.SysProcAttr = detachTTY(nil)
		out, err = runCapture(cmd)
	}

	exit := 0
	if cmd.ProcessState != nil {
		exit = cmd.ProcessState.ExitCode()
	} else if err != nil {
		exit = 1
	}
	return exit, string(out)
}
