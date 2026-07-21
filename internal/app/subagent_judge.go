package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Judgment lease: what happens when a subagent attempt outlives its elastic cap.
//
// The deterministic guards can't see intent — a churning child is event-active
// (stall watchdog blind) and a slow-model child is legitimately over the cap —
// so at lease expiry the ORCHESTRATOR's model judges the child's recent
// activity: real progress → the lease is extended; churn (repeated signatures,
// re-verification, question ping-pong) → the attempt is killed and the
// supervisor's normal restart policy applies. The judgment is bounded on every
// axis: it fires only AT expiry (zero cost for the common under-cap attempt),
// the call itself is time-capped, any error or ambiguous verdict fails safe to
// KILL (the pre-lease behavior), and an absolute backstop of
// base×subagentCapMaxFactor ends the attempt no matter what the judge says —
// a fooled judge costs at most the backstop, never the wall clock.
//
// MAGI_SUBAGENT_JUDGE=off|0|false disables judging: expiry kills immediately,
// which is exactly the pre-lease elastic-cap behavior (bench A/B knob).
const (
	judgeCallTimeout = 30 * time.Second // bound on the verdict LLM call itself
	judgeDigestCalls = 12               // recent tool calls shown to the judge
)

func subagentJudgeEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAGI_SUBAGENT_JUDGE"))) {
	case "off", "0", "false":
		return false
	}
	return true
}

// subagentBackstop is the absolute per-attempt ceiling: no judgment can extend
// past it. Identical to the elastic cap's stretch ceiling, so a slow model that
// already earned the full stretch simply has no lease left to extend.
func (a *App) subagentBackstop() time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	return time.Duration(subagentCapMaxFactor) * a.cfg.SubagentTimeout
}

// leaseExtension is one extension grant: half the configured base per verdict.
// childWaitMajority reports whether the child's recent tool calls are DOMINATED by waiting on
// the environment — a sleep/poll bash idiom (isWaitCommand) or a background-job / block-until
// poll (isPollTool: bash_output, wait_for). A subagent legitimately blocked on a long external
// operation it cannot speed up (a VM booting, a build compiling, a package installing) produces
// exactly this: repetitive polls with no deliverable motion. The lease judge would read that as
// churn and KILL — but a restart cannot boot the VM faster, so waiting must extend, not die. The
// subagent-lease counterpart of stallIsWait. Conservative: only unambiguous wait/poll verbs
// count, over the same recent window the judge sees, and it needs a real majority across at least
// two calls — so genuine task-churn (edits, varied commands, re-verification) never trips it.
func childWaitMajority(evs []event.Event, k int) bool {
	type call struct{ name, cmd string }
	var calls []call
	for _, e := range evs {
		if e.Type != event.TypePartAppended {
			continue
		}
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) != nil || d.Part.Kind != session.PartToolCall || d.Part.ToolCall == nil {
			continue
		}
		cmd := ""
		if d.Part.ToolCall.Name == "bash" {
			var ba struct {
				Command string `json:"command"`
			}
			_ = json.Unmarshal(d.Part.ToolCall.Args, &ba)
			cmd = ba.Command
		}
		calls = append(calls, call{d.Part.ToolCall.Name, cmd})
	}
	if len(calls) > k {
		calls = calls[len(calls)-k:]
	}
	if len(calls) < 2 {
		return false // too little evidence to call it a wait
	}
	waits := 0
	for _, c := range calls {
		if isPollTool(c.name) || (c.name == "bash" && isWaitCommand(c.cmd)) {
			waits++
		}
	}
	return waits*2 > len(calls) // strict majority
}

func (a *App) leaseExtension() time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg.SubagentTimeout / 2
}

// judgeLease decides whether the child, having exhausted its current lease,
// gets an extension. Returns the extension to grant (0 = kill) and a short note
// explaining the verdict (for the supervisor's transparency event — v11 bench
// forensics had kills with no visible WHY). elapsed is the attempt's age; the
// caller enforces the backstop clamp on the returned value.
func (a *App) judgeLease(ctx context.Context, parent session.Session, child session.Session, task string, elapsed time.Duration) (time.Duration, string) {
	if !subagentJudgeEnabled() {
		return 0, "judging disabled"
	}
	evs, err := a.store.Read(ctx, child.ID, 0)
	if err != nil {
		return 0, "no evidence readable: " + err.Error() // fail safe to the deterministic kill
	}
	// Deterministic wait check (before the LLM judge): a child whose recent activity is
	// dominated by waiting/polling on the environment (sleep/nc/ping, bash_output, wait_for)
	// is blocked on a long external operation it cannot speed up — extend rather than let the
	// judge misread the repetition as churn and kill a legitimate boot/build wait. Bounded by
	// the backstop like any extension; only unambiguous wait/poll verbs count (see
	// childWaitMajority), so real task-churn never trips it.
	if subagentWaitLeaseEnabled() && childWaitMajority(evs, judgeDigestCalls) {
		return a.leaseExtension(), "waiting on external op (not churn)"
	}
	digest := childToolDigest(evs, judgeDigestCalls)
	if digest == "" {
		// A child that produced NO tool activity in a whole lease is either wedged
		// mid-generation on a very slow model (the stall watchdog would have caught
		// true silence between events) or looping in pure text. Judge it anyway —
		// the transcript tail is still evidence — but with nothing to show, the
		// prompt below says so explicitly.
		digest = "(no tool calls at all this attempt)"
	}

	waitClause := ""
	if subagentWaitLeaseEnabled() {
		waitClause = "IMPORTANT: a subagent WAITING on a long external operation it cannot speed up — a VM " +
			"booting, a build compiling, a package installing, a service coming up — shows repetitive polling " +
			"or console-capturing with little visible change. That is a legitimate WAIT, not churn: a restart " +
			"cannot boot the VM or finish the build any faster, so EXTEND it. Reserve KILL for genuine task-churn " +
			"— re-editing or re-verifying already-settled work, or cycling questions without advancing. "
	}
	prompt := fmt.Sprintf(
		"You supervise a running subagent (role %q). Its task:\n%s\n\n"+
			"It has been running for %s and reached its time lease. Its recent tool activity, oldest first:\n%s\n\n"+
			"Decide from the activity alone: is it making REAL forward progress on the task (new, distinct actions "+
			"that advance the deliverable) — or churning: repeating near-identical calls, re-verifying what is "+
			"already established, or cycling questions without advancing? "+
			"%s"+
			"Reply with exactly one word: EXTEND (real progress or a legitimate wait, give it more time) or KILL (churning, restart it).",
		child.Agent, clipSpec(task, 800), fmtElapsed(elapsed), digest, waitClause)

	jctx, jcancel := context.WithTimeout(ctx, judgeCallTimeout)
	defer jcancel()
	spec := a.agentFor(parent)
	stream, err := a.providerFor(spec).StreamChat(jctx, port.ChatRequest{
		Model:  parent.Model.Model,
		System: "You are a strict execution supervisor. Judge activity evidence; answer with one word.",
		Messages: []session.Message{
			{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: prompt}}},
		},
	})
	if err != nil {
		// The judge is UNAVAILABLE (call failed), not a verdict of churn. Killing here throws away a
		// possibly-productive subagent because our supervisor couldn't reach its model — the compile-
		// compcert stall, where a heavy build slowed the judge and three subagents died on the judge's
		// own timeout. Inability to judge extends (bounded by the backstop, which still caps a runaway).
		if subagentWaitLeaseEnabled() {
			return a.leaseExtension(), "judge unavailable (call failed) — extending, backstop caps it"
		}
		return 0, "judge call failed: " + err.Error()
	}
	var reply strings.Builder
	for ev := range stream {
		if ev.Type == port.ProviderText {
			reply.WriteString(ev.Text)
		}
	}
	raw := strings.TrimSpace(reply.String())
	verdict := strings.ToUpper(raw)
	// Fail safe: only an unambiguous EXTEND extends; an actual KILL/ambiguous/garbage verdict keeps
	// the deterministic kill. But an EMPTY reply is the judge TIMING OUT — inability to judge, not a
	// churn verdict — so it extends (bounded by the backstop) rather than killing a subagent whose
	// work just outran a slow judge call (the compile-compcert three-strikes empty-reply kill).
	if strings.Contains(verdict, "EXTEND") && !strings.Contains(verdict, "KILL") {
		return a.leaseExtension(), "judge: EXTEND"
	}
	if raw == "" {
		if subagentWaitLeaseEnabled() {
			return a.leaseExtension(), "judge unavailable (empty/timeout) — extending, backstop caps it"
		}
		return 0, "judge: empty reply (timeout?)"
	}
	return 0, "judge: " + clipLine(raw, 120)
}

// childToolDigest renders the child's last k tool CALLS (name + clipped args,
// oldest first) with consecutive-duplicate annotations — the deterministic
// evidence the judge sees. Unlike turnToolEvidence it does not reset at prompt
// boundaries: nudges and ask replies inject prompts into a child session, and
// the judge needs the whole attempt's shape, not the slice since the last nudge.
func childToolDigest(evs []event.Event, k int) string {
	type call struct{ name, args string }
	var calls []call
	for _, e := range evs {
		if e.Type != event.TypePartAppended {
			continue
		}
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) != nil {
			continue
		}
		if d.Part.Kind != session.PartToolCall || d.Part.ToolCall == nil {
			continue
		}
		calls = append(calls, call{d.Part.ToolCall.Name, clipLine(string(d.Part.ToolCall.Args), 100)})
	}
	if len(calls) == 0 {
		return ""
	}
	if len(calls) > k {
		calls = calls[len(calls)-k:]
	}
	var b strings.Builder
	repeats := 0
	for i, c := range calls {
		if i > 0 && calls[i-1] == c {
			repeats++
			continue
		}
		if repeats > 0 {
			fmt.Fprintf(&b, "  (previous call repeated %d more times)\n", repeats)
			repeats = 0
		}
		fmt.Fprintf(&b, "- %s %s\n", c.name, c.args)
	}
	if repeats > 0 {
		fmt.Fprintf(&b, "  (previous call repeated %d more times)\n", repeats)
	}
	return strings.TrimRight(b.String(), "\n")
}
