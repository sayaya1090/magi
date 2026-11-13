package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// RecoveredTurn is what the start-up recovery did with one turn a previous process left open.
type RecoveredTurn struct {
	SessionID session.SessionID
	MessageID string
	// Uncertain are calls that had started (tool.started) and left no result: they may have done
	// their work. They are NOT run again; the person decides.
	Uncertain []string
	// NotRun are calls the model asked for that never started.
	NotRun []string
	// Resumable is true when nothing is uncertain: a follow-up ("continue") picks the turn up
	// from its last settled point.
	Resumable bool
	// UncertainChildren names child sessions whose interrupted work still needs checking.
	UncertainChildren []session.SessionID
	// ChildrenUnchecked marks a session whose own turn is settled but whose child sessions could
	// not be listed: the failure is reported, and the session itself is not held back by it.
	ChildrenUnchecked bool
	// Err is set when the log could not be written. Whatever was written stays; a turn whose end
	// was not written is still open, and the next start settles it again.
	Err error
}

var recoveryActor = event.Actor{Kind: event.ActorSystem, ID: "recovery"}

// RecoverInterrupted settles, in the log, the turns a previous process left open — killed,
// crashed, or past its shutdown grace. Run it at start-up, after this process holds the workspace
// (so no earlier instance is still writing) and before it takes requests.
//
// For each session whose last prompt has no ending it pairs every unanswered tool call with a
// result that says what is known — "never started" or "may have run" — and closes the turn with an
// interrupted error: resumable when nothing is uncertain, otherwise handed to the person. Nothing
// is executed again. A cancelled turn is already closed and is left alone, and so is any session
// running in this process. Writing the ending is what makes it idempotent: a second pass finds no
// open turn.
func (a *App) recoverInterrupted(ctx context.Context, workdir string) []RecoveredTurn {
	return a.recoverSelected(ctx, workdir, "")
}

func (a *App) recoverSelected(ctx context.Context, workdir string, selected session.SessionID) []RecoveredTurn {
	store := a.store
	if born, ok := store.(bornStore); ok {
		store = born.Store
	}
	var metas []session.SessionMeta
	var err error
	var inventory map[session.SessionID][]session.SessionMeta
	if scanner, ok := store.(port.SessionInventory); ok {
		metas, err = scanner.AllSessions(ctx, workdir)
		inventory = make(map[session.SessionID][]session.SessionMeta)
		for _, m := range metas {
			inventory[session.SessionID(m.Parent)] = append(inventory[session.SessionID(m.Parent)], m)
		}
	} else {
		metas, err = store.ListSessions(ctx, workdir)
	}
	if err != nil {
		return []RecoveredTurn{{Err: fmt.Errorf("list recovery sessions: %w", err)}}
	}
	if selected == "" && inventory != nil {
		var roots []session.SessionMeta
		for _, m := range metas {
			if m.Parent == "" {
				roots = append(roots, m)
			}
		}
		metas = roots
	}
	if selected != "" {
		var root []session.SessionMeta
		for _, m := range metas {
			if m.ID == selected {
				root = append(root, m)
				break
			}
		}
		metas = root
	}
	var out []RecoveredTurn
	metas, children, blocked := a.recoveryTree(ctx, workdir, metas, inventory, selected != "")
	for _, m := range metas {
		if a.runningHere(m.ID) {
			blocked[m.ID] = fmt.Errorf("session %s is still running", m.ID)
			continue
		}
		listErr := blocked[m.ID]
		evs, err := a.store.Read(ctx, m.ID, 0)
		if err != nil {
			blocked[m.ID] = err
			out = append(out, RecoveredTurn{SessionID: m.ID, Err: err})
			continue
		}
		_, _, endless := endlessInterruption(evs)
		_, pending := unfinishedTurn(evs)
		if !endless && !pending && m.Parent != "" {
			_, pending = unfinishedTurn(withoutErrors(evs))
		}
		if !endless && !pending {
			// Nothing of its own to settle. Children it could not list went unchecked, and that is
			// reported, but this session is not failed for it: it has no turn left to close.
			if listErr != nil {
				blocked[m.ID] = listErr
				out = append(out, RecoveredTurn{SessionID: m.ID, Err: listErr, ChildrenUnchecked: true})
			}
			continue
		}
		// Only the children of the turn being settled can hold it back; an older child that failed
		// is that child's failure, reported on its own row.
		if err := errors.Join(listErr, turnChildrenBlocked(evs, children[m.ID], blocked)); err != nil {
			blocked[m.ID] = err
			out = append(out, RecoveredTurn{SessionID: m.ID, Err: err})
			continue
		}
		if msgID, resumable, ok := endlessInterruption(evs); ok {
			// Shutdown may have saved the parent's interruption before its children
			// settled. Recheck their durable results before trusting that resume flag.
			r := RecoveredTurn{SessionID: m.ID, MessageID: msgID, Resumable: resumable}
			childNote, uncertain, childErr := a.recoveryChildren(ctx, evs, children[m.ID])
			r.UncertainChildren = uncertain
			switch {
			case childErr != nil:
				r.Err = childErr
			case resumable && len(uncertain) > 0:
				r.Resumable = false
				r.Err = a.closeRecovered(ctx, m.ID,
					"interrupted: child work may have run. Check the child results and their effects before continuing; nothing was re-run"+childNote, false)
			default:
				r.Err = a.finishRecovered(ctx, m.ID)
			}
			blocked[m.ID] = r.Err
			out = append(out, r)
			continue
		}
		open, ok := unfinishedTurn(evs)
		if !ok && m.Parent != "" {
			open, ok = unfinishedTurn(withoutErrors(evs))
		}
		if !ok {
			continue
		}
		r := RecoveredTurn{SessionID: m.ID, MessageID: open.MessageID}
		childNote, uncertainChildren, childErr := a.recoveryChildren(ctx, evs, children[m.ID])
		if childErr != nil {
			blocked[m.ID] = childErr
			out = append(out, RecoveredTurn{SessionID: m.ID, Err: childErr})
			continue
		}
		r.UncertainChildren = uncertainChildren
		calls, started, answered, mayHaveRun := openCalls(evs, open.MessageID)
		// A magi older than tool.started leaves none, so there no start says nothing about whether
		// a call ran — it may have done its work and died before its result was saved.
		legacy := !recordsStarts(evs)
		for _, c := range calls {
			if answered[c.CallID] {
				// An answer an earlier pass (or a shutdown) wrote as "may have run" is still that: the
				// call stays the person's to check, whatever else of the turn got written then.
				if mayHaveRun[c.CallID] {
					r.Uncertain = append(r.Uncertain, c.CallID)
				}
				continue
			}
			var text string
			uncertain := true
			switch {
			case started[c.CallID]:
				r.Uncertain = append(r.Uncertain, c.CallID)
				text = fmt.Sprintf("interrupted: magi stopped while %s was running; it may have run and its result was not saved. "+
					"It was not run again — check its effect before continuing", c.Name)
			case legacy:
				r.Uncertain = append(r.Uncertain, c.CallID)
				text = fmt.Sprintf("interrupted: magi stopped with %s unanswered, and this log is from a magi that did not "+
					"record tool starts, so it may have run and its result was not saved. It was not run again — "+
					"check its effect before continuing", c.Name)
			default:
				r.NotRun = append(r.NotRun, c.CallID)
				uncertain = false
				text = fmt.Sprintf("interrupted: magi stopped before %s started; it never started and was not run", c.Name)
			}
			if r.Err = a.appendRecoveredResult(ctx, m.ID, c.CallID, text+childNote, uncertain); r.Err != nil {
				break
			}
		}
		r.Resumable = len(r.Uncertain) == 0 && len(r.UncertainChildren) == 0
		if r.Err != nil {
			blocked[m.ID] = r.Err
			// A call without its result must not be closed over: the turn stays open and the next
			// start pairs what is still unpaired.
			out = append(out, r)
			continue
		}
		msg := "interrupted: the previous magi stopped mid-turn. Nothing after the last saved step was lost or re-run — " +
			"send a follow-up such as \"continue\" to pick it up"
		if len(r.Uncertain) > 0 {
			msg = fmt.Sprintf("interrupted: the previous magi stopped while %d tool call(s) were running (%s); they may have run. "+
				"Nothing was re-run — check their effect, then say how to continue", len(r.Uncertain), strings.Join(r.Uncertain, ", "))
		}
		if len(r.UncertainChildren) > 0 && len(r.Uncertain) == 0 {
			msg = "interrupted: child work may have run. Check the child results and their effects before continuing; nothing was re-run"
		}
		r.Err = a.closeRecovered(ctx, m.ID, msg+childNote, r.Resumable)
		blocked[m.ID] = r.Err
		out = append(out, r)
	}
	return out
}

// runningHere reports whether this process is running a turn in the session.
func (a *App) runningHere(sid session.SessionID) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.states[sid]
	return st != nil && st.cancel != nil
}

// openCalls lists the tool calls made after the prompt msgID, in order, with which of them started
// and which already have a result.
func openCalls(evs []event.Event, msgID string) (calls []session.ToolCall, started, answered, mayHaveRun map[string]bool) {
	started, answered, mayHaveRun = map[string]bool{}, map[string]bool{}, map[string]bool{}
	after := false
	for _, e := range evs {
		switch e.Type {
		case event.TypePromptSubmitted:
			var d event.PromptSubmittedData
			if json.Unmarshal(e.Data, &d) == nil {
				after = d.MessageID == msgID
			}
		case event.TypeToolStarted:
			var d event.ToolStartedData
			if after && json.Unmarshal(e.Data, &d) == nil {
				started[d.CallID] = true
			}
		case event.TypePartAppended:
			var d event.PartAppendedData
			if !after || json.Unmarshal(e.Data, &d) != nil {
				continue
			}
			switch {
			case d.Part.Kind == session.PartToolCall && d.Part.ToolCall != nil:
				calls = append(calls, *d.Part.ToolCall)
			case d.Part.Kind == session.PartToolResult && d.Part.ToolResult != nil:
				answered[d.Part.ToolResult.CallID] = true
				if d.Part.ToolResult.MayHaveRun {
					mayHaveRun[d.Part.ToolResult.CallID] = true
				}
			}
		}
	}
	return calls, started, answered, mayHaveRun
}

// closeRecovered writes the interruption, then the turn's end — in that order, so a turn is never
// read as finished without saying why. If the end cannot be written, endlessInterruption finds the
// turn on the next start and finishRecovered writes only that.
func (a *App) closeRecovered(ctx context.Context, sid session.SessionID, msg string, resumable bool) error {
	b, err := json.Marshal(event.ErrorData{Message: msg, Code: "interrupted", Resumable: resumable})
	if err != nil {
		return err
	}
	if err := a.appendFact(ctx, sid, event.TypeError, recoveryActor, b); err != nil {
		return err
	}
	return a.finishRecovered(ctx, sid)
}

func (a *App) finishRecovered(ctx context.Context, sid session.SessionID) error {
	b, err := json.Marshal(event.TurnFinishedData{Reason: "interrupted"})
	if err != nil {
		return err
	}
	return a.appendFact(ctx, sid, event.TypeTurnFinished, recoveryActor, b)
}

// appendRecoveredResult pairs one call with what recovery knows about it, and says whether that
// reached the log — which appendPart does not.
func (a *App) appendRecoveredResult(ctx context.Context, sid session.SessionID, callID, text string, mayHaveRun bool) error {
	content, err := json.Marshal(text)
	if err != nil {
		return err
	}
	d, err := json.Marshal(event.PartAppendedData{MessageID: "m_" + newID(), Role: session.RoleTool, Part: session.Part{
		ID: "p_" + newID(), Kind: session.PartToolResult,
		ToolResult: &session.ToolResult{CallID: callID, Content: content, IsError: true, MayHaveRun: mayHaveRun},
	}})
	if err != nil {
		return err
	}
	return a.appendFact(ctx, sid, event.TypePartAppended, recoveryActor, d)
}

// recordsStarts reports whether the log was written by a magi that records tool starts.
func recordsStarts(evs []event.Event) bool {
	for _, e := range evs {
		if e.Type == event.TypeToolStarted {
			return true
		}
	}
	return false
}

// endlessInterruption finds a last turn that says it was interrupted but whose end was never
// written. unfinishedTurn reads any error as an ending, so without this such a turn would stay
// unended for good, and whatever waits on turn.finished would wait forever.
func endlessInterruption(evs []event.Event) (msgID string, resumable, ok bool) {
	pending := false
	for _, e := range evs {
		switch e.Type {
		case event.TypePromptSubmitted:
			var d event.PromptSubmittedData
			if json.Unmarshal(e.Data, &d) == nil {
				msgID = d.MessageID
			}
			pending = false
		case event.TypeError:
			var d event.ErrorData
			if json.Unmarshal(e.Data, &d) == nil && d.Code == "interrupted" && !d.Recovered {
				pending, resumable = true, d.Resumable
			}
		case event.TypeTurnFinished:
			pending = false
		}
	}
	return msgID, resumable, pending && msgID != ""
}
