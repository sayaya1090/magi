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
		if a.questions[sid] == nil {
			a.questions[sid] = map[string]chan string{}
		}
		a.questions[sid][qid] = ch
		a.mu.Unlock()
		defer func() {
			a.mu.Lock()
			delete(a.questions[sid], qid)
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
	if a.grants[sid][tc.Name] {
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
	if a.perms[sid] == nil {
		a.perms[sid] = map[string]chan string{}
	}
	a.perms[sid][tc.CallID] = ch
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		delete(a.perms[sid], tc.CallID)
		a.mu.Unlock()
	}()

	rd, _ := json.Marshal(event.PermissionRequestedData{CallID: tc.CallID, Name: tc.Name, Args: tc.Args, Reason: reason})
	a.publishTransient(sid, event.TypePermissionRequested, actor, rd)

	select {
	case dec := <-ch:
		if dec == "always" || dec == "persist" {
			a.mu.Lock()
			if a.grants[sid] == nil {
				a.grants[sid] = map[string]bool{}
			}
			a.grants[sid][tc.Name] = true
			a.mu.Unlock()
			// "persist" additionally records the grant as a project allow rule
			// (`tool(**)` in .magi/config.toml), so the choice survives restarts —
			// the answer to permission-prompt fatigue for tools a project always
			// trusts (webfetch on a docs-heavy repo, bash in a scratch sandbox).
			// The session grant above already covers this run; a persist failure
			// is reported as a note, never a blocked tool.
			if rule := persistRule(tc.Name, tc.Args); dec == "persist" && rule != "" && a.cfg.PermissionPersister != nil {
				if err := a.cfg.PermissionPersister.PersistAllow(rule); err != nil {
					nd, _ := json.Marshal(event.PromptSubmittedData{
						MessageID: "m_" + newID(),
						Parts:     []session.Part{{Kind: session.PartText, Text: "note: could not persist the allow rule: " + err.Error()}},
					})
					a.appendFact(ctx, sid, event.TypePromptSubmitted, event.Actor{Kind: event.ActorSystem, ID: "loop"}, nd)
				}
			}
			return true
		}
		return dec == "allow"
	case <-ctx.Done():
		return false
	}
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
