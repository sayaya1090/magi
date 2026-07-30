package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// askUserFn builds the ToolEnv.AskUser closure for one tool call: it publishes
// a question.requested transient and blocks for the user's pick, one question
// at a time (the seq counter keys each question's channel under the call id).
// Only a top-level interactive session has a human to ask — everywhere else it
// returns nil so the ask_user tool degrades to "decide for yourself".
func (a *App) askUserFn(ctx context.Context, s session.Session, depth int, tc *session.ToolCall) func(string, []string) (string, error) {
	if depth != 0 || !a.cfg.Interactive {
		return nil
	}
	sid := s.ID
	seq := 0
	return func(question string, options []string) (string, error) {
		seq++
		qid := fmt.Sprintf("%s#%d", tc.CallID, seq)
		ch := make(chan string, 1)
		a.mu.Lock()
		if a.stateLocked(sid).questions == nil {
			a.stateLocked(sid).questions = map[string]chan string{}
		}
		a.stateLocked(sid).questions[qid] = ch
		a.mu.Unlock()
		defer func() {
			a.mu.Lock()
			delete(a.stateLocked(sid).questions, qid)
			a.mu.Unlock()
		}()
		qd, _ := json.Marshal(event.QuestionRequestedData{CallID: qid, Question: question, Options: options, Index: seq})
		a.publishTransient(sid, event.TypeQuestionRequested, event.Actor{Kind: event.ActorSystem, ID: "loop"}, qd)
		select {
		case ans := <-ch:
			return ans, nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

// requestPermission applies the permission policy, blocking for an interactive
// decision when policy is "ask" (F-LOOP-PERMISSION).
func (a *App) requestPermission(ctx context.Context, sid session.SessionID, actor event.Actor, tc *session.ToolCall, forcePrompt bool, reason string) bool {
	// A policy-forced prompt (risky bash, egress) overrides allow/auto so the
	// user always gets a say — but an explicit "deny" mode still denies.
	if !forcePrompt {
		switch a.Permission() {
		case "allow":
			return true
		case "deny":
			return false
		case "auto":
			// Accept-edits: file modifications are auto-approved, but commands and
			// network access (bash/webfetch) still prompt — the convenient default
			// for an editing session without going full YOLO.
			if fileModifiers[tc.Name] {
				return true
			}
			// Non-edit tools fall through to the interactive "ask" path below.
		}
	} else if a.Permission() == "deny" {
		return false
	}
	// "ask" (and "auto" for non-edit tools): honor a prior "always" grant.
	a.mu.Lock()
	if st, ok := a.stateIf(sid); ok && st.grants[tc.Name] {
		a.mu.Unlock()
		return true
	}
	// No human to ask (headless/automation): never block on an interactive prompt —
	// resolve by policy. "allow" grants (allow = allow-all, the headless default);
	// "ask"/"auto" deny (the safe default when there's no one to approve). This is what
	// prevents the deadlock where a forced prompt waits forever on a decision that can't
	// come (the run/bus goroutines then all sleep → the Go runtime kills the process).
	if !a.cfg.Interactive {
		a.mu.Unlock()
		return a.Permission() == "allow"
	}
	ch := make(chan string, 1)
	if a.stateLocked(sid).perms == nil {
		a.stateLocked(sid).perms = map[string]chan string{}
	}
	a.stateLocked(sid).perms[tc.CallID] = ch
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		delete(a.stateLocked(sid).perms, tc.CallID)
		a.mu.Unlock()
	}()

	rd, _ := json.Marshal(event.PermissionRequestedData{CallID: tc.CallID, Name: tc.Name, Args: tc.Args, Reason: reason})
	a.publishTransient(sid, event.TypePermissionRequested, actor, rd)

	select {
	case dec := <-ch:
		if dec == "always" || dec == "persist" {
			a.mu.Lock()
			if a.stateLocked(sid).grants == nil {
				a.stateLocked(sid).grants = map[string]bool{}
			}
			a.stateLocked(sid).grants[tc.Name] = true
			a.mu.Unlock()
			// "persist" additionally records the grant as a project allow rule
			// (`tool(**)` in .magi/config.toml), so the choice survives restarts —
			// the answer to permission-prompt fatigue for tools a project always
			// trusts (webfetch on a docs-heavy repo, bash in a scratch sandbox).
			// The session grant above already covers this run; nothing here ever
			// blocks the tool, it only reports.
			if dec == "persist" {
				a.notePersistOutcome(ctx, sid, tc)
			}
			return true
		}
		return dec == "allow"
	case <-ctx.Done():
		return false
	}
}

// notePersistOutcome carries out the "project" choice and — whenever it did NOT happen — says so.
//
// The button is labelled `project`: the user is told the approval is being written where the
// project keeps it, so it survives a restart. Three things can stop that, and only one of them
// used to speak. A PersistAllow error was reported; a bash command with no stable program name to
// pin a rule to was declined by design, and a run with no project config to write to had nowhere
// to put it — both silently. In each of those the SESSION grant still stands, so nothing looks
// wrong until the next run asks again, and by then the choice that was supposed to prevent the
// prompt is long out of sight.
//
// Declining to write `bash(**)` for a command whose first token is a shell construct is the right
// call — a blanket bash grant is exactly what the narrowing exists to avoid. Saying nothing about
// it is not.
func (a *App) notePersistOutcome(ctx context.Context, sid session.SessionID, tc *session.ToolCall) {
	note := ""
	switch rule := persistRule(tc.Name, tc.Args); {
	case a.cfg.PermissionPersister == nil:
		note = "note: this run has no project config to write to, so `" + tc.Name +
			"` is approved for the rest of THIS SESSION only — a later run will ask again."
	case rule == "":
		note = "note: this command opens with a shell construct rather than a program name, so " +
			"there is no stable prefix to pin a project rule to and nothing was written " +
			"(a blanket `bash(**)` would pre-approve every future command, which is what the " +
			"narrowing avoids). It is approved for the rest of THIS SESSION; a later run will ask again."
	default:
		if err := a.cfg.PermissionPersister.PersistAllow(rule); err != nil {
			note = "note: could not persist the allow rule " + rule + ": " + err.Error() +
				" — it is approved for the rest of THIS SESSION only."
		}
	}
	if note == "" {
		return // written; the modal already told the user that is what `project` does
	}
	nd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m_" + newID(),
		Parts:     []session.Part{{Kind: session.PartText, Text: note}},
	})
	a.appendFact(ctx, sid, event.TypePromptSubmitted, event.Actor{Kind: event.ActorSystem, ID: "loop"}, nd)
}

// persistRule builds the project allow rule recorded for a "persist" decision.
// For most tools it grants the whole tool (`tool(**)`). For bash — where a
// blanket `bash(**)` would silently pre-approve every future command, including
// destructive ones — it narrows to a command-PREFIX rule (`bash(<cmd>:*)`): the
// user approved `curl ...`, so persist `bash(curl:*)`, not carte blanche. The
// prefix is the leading run of safe command tokens (argv words up to the first
// shell metacharacter), so `bash(git status:*)` persists but a piped or chained
// command falls back to the first token only. If no usable prefix is found the
// grant stays session-only (empty rule → caller no-ops) rather than over-granting.
func persistRule(tool string, args json.RawMessage) string {
	if strings.ToLower(tool) != "bash" {
		return tool + "(**)"
	}
	var m struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(args, &m)
	prefix := safeCommandPrefix(m.Command)
	if prefix == "" {
		return "" // no safe prefix → do not persist a blanket bash grant
	}
	return "bash(" + prefix + ":*)"
}

// safeCommandPrefix returns the program name of a shell command — the first
// argv word — provided the command opens with a plain literal and not a shell
// metacharacter (a leading pipe/redirect/subshell has no stable "program" to
// pin to). Persisting the executable name (`curl`, `git`) is deliberately
// coarse-but-safe: it survives variable arguments (URLs, paths) that a longer
// prefix would bake in, while the destructive/egress scanners still re-prompt on
// dangerous invocations of that same program. Returns "" for an empty command
// or one that starts with a metacharacter.
func safeCommandPrefix(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ""
	}
	first := strings.Fields(cmd)[0]
	if strings.ContainsAny(first, "|&;><`$(){}*?!\\\"'") {
		return ""
	}
	return first
}
