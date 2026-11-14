package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// RecoveryKind is what a conversation needs from the person after a restart.
type RecoveryKind string

const (
	// RecoveryNone: nothing was interrupted, or a newer prompt has moved on from it.
	RecoveryNone RecoveryKind = "none"
	// RecoveryResumable: the last turn was interrupted and nothing in it is uncertain; a follow-up
	// ("continue") picks it up from the last saved step.
	RecoveryResumable RecoveryKind = "resumable"
	// RecoveryNeedsCheck: calls in the last turn may have run. Each is the person's to check.
	RecoveryNeedsCheck RecoveryKind = "needs_check"
	// RecoveryFailed: recovery could not settle this conversation. It takes no new work until
	// recovery is retried and succeeds.
	RecoveryFailed RecoveryKind = "recovery_failed"
)

// Verdict is what the person decided about a call recovery left uncertain.
type Verdict string

const (
	VerdictRan      Verdict = "ran"      // it did its work; do not repeat it
	VerdictNotRan   Verdict = "not_ran"  // it had no effect
	VerdictRerunOK  Verdict = "rerun_ok" // repeating it is fine
	VerdictDeferred Verdict = "deferred" // not decided yet; still the person's to check
)

func (v Verdict) valid() bool {
	return v == VerdictRan || v == VerdictNotRan || v == VerdictRerunOK || v == VerdictDeferred
}

// settles reports whether the verdict takes the call off the person's list.
func (v Verdict) settles() bool { return v == VerdictRan || v == VerdictNotRan || v == VerdictRerunOK }

// UncertainCall is one call the person has to check: what it was and what it touched.
type UncertainCall struct {
	Details string  `json:"details,omitempty"`
	CallID  string  `json:"callId"`
	Tool    string  `json:"tool"`
	Target  string  `json:"target,omitempty"`
	Verdict Verdict `json:"verdict,omitempty"`
}

// RecoveryState is the recovery standing of one conversation, as every screen should show it.
type RecoveryState struct {
	Task          string       `json:"task,omitempty"`
	LastCompleted string       `json:"lastCompleted,omitempty"`
	Kind          RecoveryKind `json:"kind"`
	// MessageID is the prompt of the interrupted turn: the last settled point a follow-up resumes.
	MessageID string    `json:"messageId,omitempty"`
	At        time.Time `json:"at,omitempty"`
	// Uncertain lists the calls still waiting for the person, in the order the model made them.
	Uncertain []UncertainCall `json:"uncertain,omitempty"`
	// Reason is why recovery failed, or what the interruption record says.
	Reason string `json:"reason,omitempty"`
}

// ErrRecoveryPending refuses new work in a conversation recovery could not settle.
var ErrRecoveryPending = errors.New("recovery of this conversation has not finished")

// recoveryBook keeps what the last recovery pass could not do. It lives in the process because a
// failure to write the log cannot be read back from that log.
type recoveryBook struct {
	mu       sync.Mutex
	workdir  string
	lazy     bool
	checked  map[session.SessionID]bool
	failed   map[session.SessionID]string
	unlisted string
}

var resolverActor = event.Actor{Kind: event.ActorUser, ID: "person"}

// RecoverInterrupted runs recovery (see recoverInterrupted) and keeps its failures, so a
// conversation it could not settle reads as recovery_failed and refuses new work until retried.
// It holds recoverGate for writing: no prompt can be stored and left unstarted while it reads.
func (a *App) RecoverInterrupted(ctx context.Context, workdir string) []RecoveredTurn {
	a.recoverGate.Lock()
	defer a.recoverGate.Unlock()
	out := a.recoverInterrupted(ctx, workdir)
	a.recovery.mu.Lock()
	defer a.recovery.mu.Unlock()
	a.recovery.workdir = workdir
	a.recovery.failed = map[session.SessionID]string{}
	a.recovery.unlisted = ""
	for _, r := range out {
		switch {
		case r.Err == nil || r.ChildrenUnchecked:
		case r.SessionID == "":
			a.recovery.unlisted = r.Err.Error()
		default:
			a.recovery.failed[r.SessionID] = r.Err.Error()
		}
	}
	return out
}

// RecoverSelected enables lazy recovery and checks only the opened conversation.
// Other sessions are checked when subscribed to or submitted to in this process.
func (a *App) RecoverSelected(ctx context.Context, workdir string, sid session.SessionID) []RecoveredTurn {
	a.recovery.mu.Lock()
	a.recovery.workdir = workdir
	a.recovery.lazy = true
	a.recovery.mu.Unlock()
	return a.ensureSelectedRecovery(ctx, sid, false)
}

// CheckSessionRecovery is used by session selection before exposing its saved state.
func (a *App) CheckSessionRecovery(ctx context.Context, sid session.SessionID) {
	a.ensureSelectedRecovery(ctx, sid, false)
}

func (a *App) ensureSelectedRecovery(ctx context.Context, sid session.SessionID, force bool) []RecoveredTurn {
	a.recoverGate.Lock()
	defer a.recoverGate.Unlock()
	a.recovery.mu.Lock()
	wd, enabled, checked := a.recovery.workdir, a.recovery.lazy, a.recovery.checked[sid]
	a.recovery.mu.Unlock()
	if !enabled || sid == "" || (checked && !force) || a.runningHere(sid) {
		return nil
	}
	out := a.recoverSelected(ctx, wd, sid)
	a.recovery.mu.Lock()
	defer a.recovery.mu.Unlock()
	if a.recovery.checked == nil {
		a.recovery.checked = map[session.SessionID]bool{}
	}
	if a.recovery.failed == nil {
		a.recovery.failed = map[session.SessionID]string{}
	}
	delete(a.recovery.failed, sid)
	a.recovery.checked[sid] = true
	for _, r := range out {
		target := r.SessionID
		if target == "" {
			target = sid
		}
		a.recovery.checked[target] = true
		if r.Err != nil && !r.ChildrenUnchecked {
			a.recovery.failed[target] = r.Err.Error()
		} else {
			delete(a.recovery.failed, target)
		}
	}
	return out
}

// RetryRecovery runs recovery again over the workspace the last pass covered and returns what it
// did for sid. Recovery is idempotent, so a session already settled is not touched again.
func (a *App) RetryRecovery(ctx context.Context, sid session.SessionID) RecoveredTurn {
	a.recovery.mu.Lock()
	wd := a.recovery.workdir
	lazy := a.recovery.lazy
	a.recovery.mu.Unlock()
	if wd == "" {
		return RecoveredTurn{SessionID: sid, Err: errors.New("recovery has not run in this process")}
	}
	out := []RecoveredTurn(nil)
	if lazy {
		out = a.ensureSelectedRecovery(ctx, sid, true)
	} else {
		out = a.RecoverInterrupted(ctx, wd)
	}
	for _, r := range out {
		if r.SessionID == "" && r.Err != nil {
			return RecoveredTurn{SessionID: sid, Err: r.Err}
		}
		if r.SessionID == sid && !r.ChildrenUnchecked {
			return r
		}
	}
	return RecoveredTurn{SessionID: sid}
}

func (a *App) recoveryFailure(sid session.SessionID) (string, bool) {
	a.recovery.mu.Lock()
	defer a.recovery.mu.Unlock()
	if a.recovery.unlisted != "" {
		return "could not list the conversations to recover: " + a.recovery.unlisted, true
	}
	why, ok := a.recovery.failed[sid]
	return why, ok
}

// refuseUnrecovered is the gate Submit and Steer pass first.
func (a *App) refuseUnrecovered(sid session.SessionID) error {
	if why, ok := a.recoveryFailure(sid); ok {
		return fmt.Errorf("%w: %s. Retry recovery before sending new work", ErrRecoveryPending, why)
	}
	return nil
}

// RecoveryStateOf reports what the conversation needs from the person after a restart. It reads
// the log as it stands, so every client and every restart get the same answer; only a recovery
// failure, which could not be written there, comes from this process.
func (a *App) RecoveryStateOf(ctx context.Context, sid session.SessionID) RecoveryState {
	if why, ok := a.recoveryFailure(sid); ok {
		return RecoveryState{Kind: RecoveryFailed, Reason: why}
	}
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return RecoveryState{Kind: RecoveryFailed, Reason: "could not read the conversation: " + err.Error()}
	}
	return recoveryState(evs)
}

func recoveryState(evs []event.Event) RecoveryState {
	var st RecoveryState
	calls := map[string]session.ToolCall{}
	results := map[string]string{}
	var order []string
	mayHaveRun := map[string]bool{}
	verdict := map[string]Verdict{}
	interrupted, resumable := false, false
	for _, e := range evs {
		switch e.Type {
		case event.TypePromptSubmitted:
			var d event.PromptSubmittedData
			if json.Unmarshal(e.Data, &d) != nil {
				continue
			}
			st = RecoveryState{MessageID: d.MessageID}
			results = map[string]string{}
			for _, part := range d.Parts {
				if part.Kind == session.PartText && part.Text != "" {
					st.Task = recoveryExcerpt(part.Text)
					break
				}
			}
			calls, order, mayHaveRun, verdict = map[string]session.ToolCall{}, nil, map[string]bool{}, map[string]Verdict{}
			interrupted, resumable = false, false
		case event.TypePartAppended:
			var d event.PartAppendedData
			if json.Unmarshal(e.Data, &d) != nil {
				continue
			}
			switch {
			case d.Part.Kind == session.PartToolCall && d.Part.ToolCall != nil:
				if _, seen := calls[d.Part.ToolCall.CallID]; !seen {
					order = append(order, d.Part.ToolCall.CallID)
				}
				calls[d.Part.ToolCall.CallID] = *d.Part.ToolCall
			case d.Part.Kind == session.PartToolResult && d.Part.ToolResult != nil:
				result := d.Part.ToolResult
				results[result.CallID] = string(result.Content)
				if result.MayHaveRun {
					mayHaveRun[result.CallID] = true
				} else if !result.IsError {
					if call, ok := calls[result.CallID]; ok {
						st.LastCompleted = recoveryExcerpt(call.Name + " " + callTarget(call.Args))
					}
				}
			}
		case event.TypeError:
			var d event.ErrorData
			if json.Unmarshal(e.Data, &d) == nil && d.Code == "interrupted" && !d.Recovered {
				interrupted, resumable = true, d.Resumable
				st.At, st.Reason = e.TS, d.Message
			}
		case event.TypeToolResolved:
			var d event.ToolResolvedData
			if json.Unmarshal(e.Data, &d) == nil {
				verdict[d.CallID] = Verdict(d.Verdict)
			}
		}
	}
	if !interrupted {
		return RecoveryState{Kind: RecoveryNone}
	}
	for _, id := range order {
		if !mayHaveRun[id] || verdict[id].settles() {
			continue
		}
		c := calls[id]
		st.Uncertain = append(st.Uncertain, UncertainCall{Details: string(c.Args) + "\n\n" + results[id], CallID: id, Tool: c.Name, Target: callTarget(c.Args), Verdict: verdict[id]})
	}
	switch {
	case len(st.Uncertain) > 0:
		st.Kind = RecoveryNeedsCheck
	case resumable || len(mayHaveRun) > 0:
		// Nothing was uncertain, or the person has settled every call that was.
		st.Kind = RecoveryResumable
	default:
		// Not resumable with no call of its own in doubt: child work may have run.
		st.Kind = RecoveryNeedsCheck
	}
	return st
}

// ResolveCall records the person's verdict on a call recovery left uncertain. Only a call still on
// the list can be resolved; deferring keeps it there.
func (a *App) ResolveCall(ctx context.Context, sid session.SessionID, callID string, v Verdict, note string) error {
	if !v.valid() {
		return fmt.Errorf("unknown verdict %q (want ran, not_ran, rerun_ok or deferred)", v)
	}
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return err
	}
	found := false
	for _, u := range recoveryState(evs).Uncertain {
		if u.CallID == callID {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("%s is not a call waiting for your check in %s", callID, sid)
	}
	b, err := json.Marshal(event.ToolResolvedData{CallID: callID, Verdict: string(v), Note: note})
	if err != nil {
		return err
	}
	return a.appendFact(ctx, sid, event.TypeToolResolved, resolverActor, b)
}

// callTarget is the one line a person needs to recognise what a call touched.
func callTarget(args json.RawMessage) string {
	var m map[string]any
	if json.Unmarshal(args, &m) != nil {
		return ""
	}
	for _, k := range []string{"command", "path", "file_path", "url", "pattern", "query"} {
		if s, ok := m[k].(string); ok && s != "" {
			line, _, _ := strings.Cut(s, "\n")
			if r := []rune(line); len(r) > 80 {
				line = string(r[:80]) + "…"
			}
			return line
		}
	}
	return ""
}

// recheckTools only look; repeating one cannot do anything twice, so recovery never holds them.
var recheckTools = map[string]bool{"read": true, "grep": true, "glob": true, "list": true}

type repeatKey struct{}

// asRepeat marks a permission request as a repeat of an uncertain call: no standing "always"
// grant answers it, because the grant was given before anyone knew the first one may have run.
func asRepeat(ctx context.Context) context.Context { return context.WithValue(ctx, repeatKey{}, true) }

func isRepeat(ctx context.Context) bool { v, _ := ctx.Value(repeatKey{}).(bool); return v }

// uncertainRepeat finds the call recovery left uncertain that tc would do again: the same tool with
// the same arguments, not cleared by the person as rerun_ok or not_ran. A call the person said RAN
// still counts — repeating it does the work twice. It returns why to ask, or "" when tc is new.
func (a *App) uncertainRepeat(ctx context.Context, sid session.SessionID, tc *session.ToolCall) string {
	if recheckTools[tc.Name] {
		return ""
	}
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return fmt.Sprintf("magi could not read this conversation to check whether this %s repeats a call that may already have run (%v)", tc.Name, err)
	}
	calls := map[string]session.ToolCall{}
	var uncertain []string
	verdict := map[string]Verdict{}
	for _, e := range evs {
		switch e.Type {
		case event.TypePartAppended:
			var d event.PartAppendedData
			if json.Unmarshal(e.Data, &d) != nil {
				continue
			}
			if d.Part.Kind == session.PartToolCall && d.Part.ToolCall != nil {
				calls[d.Part.ToolCall.CallID] = *d.Part.ToolCall
			}
			if d.Part.Kind == session.PartToolResult && d.Part.ToolResult != nil && d.Part.ToolResult.MayHaveRun {
				uncertain = append(uncertain, d.Part.ToolResult.CallID)
			}
		case event.TypeToolResolved:
			var d event.ToolResolvedData
			if json.Unmarshal(e.Data, &d) == nil {
				verdict[d.CallID] = Verdict(d.Verdict)
			}
		}
	}
	want := canonicalArgs(tc.Args)
	for _, id := range uncertain {
		if v := verdict[id]; v == VerdictRerunOK || v == VerdictNotRan {
			continue
		}
		c, ok := calls[id]
		if !ok || c.Name != tc.Name || canonicalArgs(c.Args) != want {
			continue
		}
		if verdict[id] == VerdictRan {
			return fmt.Sprintf("this repeats %s (%s), which you said already ran before magi stopped; allowing it does it again", tc.Name, id)
		}
		return fmt.Sprintf("this repeats %s (%s), which may already have run before magi stopped; check its effect before allowing it again", tc.Name, id)
	}
	return ""
}

// Bound UI excerpts without splitting UTF-8; full details remain in the transcript.
func recoveryExcerpt(text string) string {
	chars := []rune(strings.Join(strings.Fields(text), " "))
	if len(chars) > 240 {
		return string(chars[:240]) + "…"
	}
	return string(chars)
}
