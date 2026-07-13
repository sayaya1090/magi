package app

import (
	"os"
	"strings"
)

// council_means.go — Rung 1 of stuck-concern escalation (see
// docs/proposals/council-stuck-concern-escalation.md).
//
// When the termination council holds a turn open for several rounds, the objection alone
// has demonstrably failed to move the agent. The pypi-server bench trial is the motivating
// case: the council correctly refused "done" because the graded check runs a live
// `pip install --index-url http://localhost:8080/simple`, but the agent could not keep a
// server running (every `... &` died with its one-shot shell) and churned to the wall clock
// writing README/notes instead. The council was right; the agent lacked the operational
// MEANS. Rung 1 appends a concrete, task-agnostic "how to satisfy this" recipe — derived
// only from the feedback's own keywords — to the re-injected objection, so the next attempt
// has the missing move. It never weakens the gate (it augments the objection, never approves)
// and is recorded separately from the raw feedback so repeat-detection is unaffected.

// councilMeansRound is the council round at (and after) which a means recipe is appended.
// From the FIRST rejection: bench forensics (openssl/pypi round-cost analysis, 2026-07-13)
// showed the wasted cycle is round 1 → an untargeted rework phase → round 2 rejection for
// the same concern; giving the recipe up front converts that into one targeted action.
const councilMeansRound = 1

// councilMeansEnabled gates Rung 1 (default ON): MAGI_COUNCIL_MEANS=off|0|false|no
// disables it (bench A/B knob) — e.g. to reproduce the historical plain-objection
// feedback byte-for-byte.
func councilMeansEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAGI_COUNCIL_MEANS"))) {
	case "off", "0", "false", "no":
		return false
	}
	return true
}

// meansHint returns a concrete, task-agnostic recipe for satisfying the council's objection,
// selected by keyword from the feedback itself, or "" when no category matches (leave the
// objection unchanged). The recipes are generic operational knowledge — how to keep a process
// alive, how to produce real evidence — never a task's answer, so injecting them cannot leak
// solutions into a benchmark. Pure function of its input; fully unit-testable.
func meansHint(feedback string) string {
	fb := strings.ToLower(feedback)
	has := func(subs ...string) bool {
		for _, s := range subs {
			if strings.Contains(fb, s) {
				return true
			}
		}
		return false
	}

	var parts []string

	// Category 1 — a process/server/daemon must stay up (survive the command that started it).
	// This is the exact gap the pypi trial hit: the check connects to a live port, but the agent
	// started the server in the foreground or in a one-shot `&` that died immediately.
	if has("server", "daemon", "listen", " port", "running", "serve", "background", "http://", "https://", "localhost", "127.0.0.1") {
		parts = append(parts,
			"To keep a process alive after the starting command returns, launch it DETACHED, not in the foreground and not as a bare `cmd &` inside a one-shot shell (that dies with the shell), and not `timeout N cmd` (that exits after N):\n"+
				"    setsid <cmd> >/tmp/svc.log 2>&1 </dev/null &   # or: nohup <cmd> & disown\n"+
				"Then PROVE it is actually listening before claiming done, e.g. `sleep 1 && curl -sf <url>` or `ss -ltnp | grep <port>` (non-zero exit = not up, keep fixing).")
	}

	// Category 2 — the council wants real evidence, not a claim/description/simulation. The pypi
	// agent kept writing "how it would work" docs; the fix is to RUN the real end-to-end check.
	if has("evidence", "prove", "proof", "demonstrate", "actually", "not shown", "no proof", "verify that", "confirm", "install", "build succeeds", "tests pass") {
		parts = append(parts,
			"Do not describe, assert, or simulate success — RUN the real end-to-end command an evaluator would run (the exact install/build/test/request), and show its command line, exit status, and output. A README or a script explaining how it *would* work is not evidence; only a real invocation that exits 0 is.")
	}

	if len(parts) == 0 {
		return ""
	}
	return "Means (task-agnostic guidance, not a new requirement — how to satisfy the point above):\n" + strings.Join(parts, "\n")
}
