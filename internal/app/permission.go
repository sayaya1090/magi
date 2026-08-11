package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/report"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// askUserFn builds the ToolEnv.AskUser closure for one tool call: it publishes
// a question.requested transient and blocks for the user's pick, one question
// at a time (the seq counter keys each question's channel under the call id).
// Only a top-level interactive session has a human to ask — everywhere else it
// returns nil so the ask_user tool degrades to "decide for yourself".
func (a *App) askUserFn(ctx context.Context, s session.Session, depth int, tc *session.ToolCall) func(port.Question) (string, error) {
	if depth != 0 || !a.cfg.Interactive {
		return nil
	}
	sid := s.ID
	return func(q port.Question) (string, error) {
		question, options, grounds := q.Text, q.Options, q.Grounds
		// The id is the call plus the position, so two questions of one call are two prompts a
		// viewer can answer separately. It was a counter kept here; the tool knows the position and
		// now says it, which leaves one source instead of two that agree until they do not.
		qid := fmt.Sprintf("%s#%d", tc.CallID, q.Index)
		ch := make(chan string, 1)
		a.mu.Lock()
		if a.stateLocked(sid).questions == nil {
			a.stateLocked(sid).questions = map[string]chan string{}
		}
		a.stateLocked(sid).questions[qid] = ch
		a.noteAskingLocked(sid, qid, Ask{ID: qid, Kind: "question", What: question, Options: options,
			Report: grounds, Index: q.Index, Total: q.Total, Since: time.Now()})
		a.mu.Unlock()
		defer func() {
			a.mu.Lock()
			delete(a.stateLocked(sid).questions, qid)
			delete(a.stateLocked(sid).asking, qid)
			a.mu.Unlock()
		}()
		qd, _ := json.Marshal(event.QuestionRequestedData{CallID: qid, Question: question, Options: options,
			Report: grounds, Index: q.Index, Total: q.Total})
		a.publishTransient(sid, event.TypeQuestionRequested, event.Actor{Kind: event.ActorSystem, ID: "loop"}, qd)
		var expired <-chan time.Time
		if bound := a.answerBound(); bound > 0 {
			t := time.NewTimer(bound)
			defer t.Stop()
			expired = t.C
		}
		select {
		case ans := <-ch:
			return ans, nil
		case <-expired:
			// The tool degrades to "decide for yourself", which is what it does anywhere there is
			// no human — but the agent is TOLD, so it does not treat silence as an answer.
			return "", fmt.Errorf("nobody answered within %s; no UI is attached — decide for yourself and say which way you went", a.answerBound())
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
	a.noteAskingLocked(sid, tc.CallID, Ask{ID: tc.CallID, Kind: "permission", What: tc.Name, Args: tc.Args, Reason: reason, Since: time.Now()})
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		delete(a.stateLocked(sid).perms, tc.CallID)
		delete(a.stateLocked(sid).asking, tc.CallID)
		a.mu.Unlock()
	}()

	rd, _ := json.Marshal(event.PermissionRequestedData{CallID: tc.CallID, Name: tc.Name, Args: tc.Args, Reason: reason})
	a.publishTransient(sid, event.TypePermissionRequested, actor, rd)

	// A bounded wait when the answerer is in another process (see Config.AnswerWait). The timer is
	// only armed when one is configured, so the terminal's prompt still waits as long as the person
	// in front of it needs.
	var expired <-chan time.Time
	if bound := a.answerBound(); bound > 0 {
		t := time.NewTimer(bound)
		defer t.Stop()
		expired = t.C
	}
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
	case <-expired:
		// Nobody answered. Resolve the way a run with no human resolves — by policy — and record
		// that this is what happened: a decision taken by default reads identically to one somebody
		// made unless the log says otherwise.
		byPolicy := a.Permission() == "allow"
		a.noteUnanswered(ctx, sid, tc, byPolicy)
		return byPolicy
	case <-ctx.Done():
		return false
	}
}

// answerBound is how long THIS prompt waits, read from the mode as it stands now.
//
// Config.AnswerWait says whether an answerer is somewhere else at all — a property of the process,
// decided when it started: a terminal has the person in front of it and waits, a daemon has whoever
// attaches and cannot wait on them forever in every mode.
//
// Which modes it applies to is decided HERE, per prompt, because the mode changes while the process
// runs: Shift+Tab in an attached terminal, /permission, or SetPermission over the socket. Frozen at
// startup, a companion switched from auto to ask would go on resolving prompts by timer — which is
// the one thing ask exists to prevent — and one switched the other way would hang on a prompt it
// was told to give up on.
//
//   - ask   — no bound. Choosing to be asked and then being answered by a timer is not being asked.
//   - auto  — bounded, where an answerer is elsewhere. The prompts left in auto are commands and
//     the network, where carrying on without an answer is defensible.
//   - allow — bounded too, and this is not a contradiction. Allow does not prompt on its own, but a
//     guardrail can force one over the top of it (a risky command, egress). That prompt exists
//     BECAUSE of the policy rather than because of the mode, and hanging a companion whose operator
//     asked for "allow" on a question they never asked to be asked is the wrong way to be careful.
//     It resolves the way the mode says — allow — and is written down as a default rather than a
//     decision.
func (a *App) answerBound() time.Duration {
	if a.cfg.AnswerWait > 0 && a.Permission() != "ask" {
		return a.cfg.AnswerWait
	}
	return 0
}

// noteUnanswered records a prompt that timed out with no answer.
func (a *App) noteUnanswered(ctx context.Context, sid session.SessionID, tc *session.ToolCall, allowed bool) {
	verdict := "denied"
	if allowed {
		verdict = "allowed"
	}
	// WithoutCancel: the turn's context may be the very thing being torn down, and a record of why
	// a tool was allowed or denied is worth more than the turn it belonged to.
	_ = a.appendPromptText(context.WithoutCancel(ctx), sid,
		event.Actor{Kind: event.ActorSystem, ID: "permission"}, fmt.Sprintf(
			"no UI answered the permission prompt for %s within %s — %s by the %q policy",
			tc.Name, a.answerBound(), verdict, a.Permission()))
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

// Ask is a prompt the engine is blocked on, for a process that cannot see the transient event that
// announced it.
//
// A permission prompt and an ask_user question live in this process's memory and nowhere else — not
// in the log, because they are not facts about what happened but a question about what should. So a
// dashboard reading the log sees an agent with an open turn that has stopped moving, which looks
// exactly like a slow build. This is the one thing a second process genuinely cannot work out for
// itself, which is the same test the five write calls had to pass.
type Ask struct {
	// ID is the call id an answer must carry. Without it the prompt is legible and unanswerable:
	// a viewer could say a permission is pending and have no way to grant it, which is a worse
	// place to stop than not showing it at all.
	ID   string
	Kind string // "permission" | "question"
	What string // the tool being asked about, or the question itself
	// Args, Reason and Options are the rest of the request. A prompt that arrived as "permission:
	// bash" is not one anybody can answer: what is being decided is the COMMAND, and the policy's
	// reason for stopping on it is what the decision is supposed to be made on. Carrying them means
	// a viewer in another process can draw the same prompt the terminal draws, rather than a
	// summary of it.
	Args    json.RawMessage
	Reason  string
	Options []string
	// Report is why the decision is being put to a person: what the agent tried, what each option
	// costs, which way it leans — whatever the decision-report skill asks for. It travels with the
	// prompt because a viewer in another process has no other way to reach it, and a question
	// without it is the thing this was built to stop.
	Report []report.Filled
	// Index and Total place this question in the run its call is asking, counting from one. A tool
	// may ask several and each one blocks, so a person answering the first is entitled to know
	// that two more are coming.
	Index, Total int
	Since        time.Time // when it was asked, so a viewer can say how long it has been waiting
}

// noteAskingLocked records an open prompt. Caller holds a.mu.
func (a *App) noteAskingLocked(sid session.SessionID, id string, ask Ask) {
	st := a.stateLocked(sid)
	if st.asking == nil {
		st.asking = map[string]Ask{}
	}
	st.asking[id] = ask
}

// Waiting reports the prompt a session has been blocked on longest, if any.
//
// The oldest rather than any: with more than one open (a question asked while a permission prompt
// stands) the one that has been waiting longest is the one holding everything else up.
func (a *App) Waiting(sid session.SessionID) (Ask, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st, ok := a.stateIf(sid)
	if !ok {
		return Ask{}, false
	}
	var oldest Ask
	found := false
	for _, ask := range st.asking {
		if !found || ask.Since.Before(oldest.Since) {
			oldest, found = ask, true
		}
	}
	return oldest, found
}
