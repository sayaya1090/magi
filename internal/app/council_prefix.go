package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/core/text"
	"github.com/sayaya1090/magi/internal/port"
)

const councilPrefixCap = 64 * 1024

// councilPrefix is chronological, not a rolling summary rewritten each round.
// Reports and previous verdicts keep their identities; neither is tool evidence.
// Overflow starts a new cache prefix. Current-state evidence is always supplied
// separately by councilAdvice, so this history never replaces fresh observations.
func councilPrefix(evs []event.Event) string {
	start := 0
	for i, e := range evs {
		if e.Type == event.TypeTurnFinished {
			start = i + 1
		}
	}
	const header = "# Chronological turn history\nHistorical state may be superseded by later results and the current review snapshot.\n"
	const omitted = "[Earlier history omitted to keep the cache prefix within its size limit.]\n"
	dropped := false
	var entries []string
	calls := map[string]toolCallBrief{}
	size := len(header) + len(omitted)
	for _, e := range evs[start:] {
		label, body := "", ""
		switch e.Type {
		case event.TypePromptSubmitted:
			if e.Actor.Kind == event.ActorUser {
				label = "User request"
				body = string(e.Data)
			}
		case event.TypePartAppended:
			var d event.PartAppendedData
			if json.Unmarshal(e.Data, &d) != nil {
				continue
			}
			switch d.Part.Kind {
			case session.PartToolCall:
				if c := d.Part.ToolCall; c != nil {
					calls[c.CallID] = toolCallBrief{name: c.Name, args: evidenceArgs(c.Name, c.Args)}
				}
			case session.PartToolResult:
				if r := d.Part.ToolResult; r != nil {
					b := calls[r.CallID]
					label = "Tool result"
					status := "ok"
					if r.IsError {
						status = "error"
					}
					if b.name == "council" {
						label = "Previous council response (judgment, not execution evidence)"
					}
					body = evidenceLine(b, status, toolResultText(r.Content))
				}
			case session.PartText:
				if d.Role == session.RoleAssistant {
					label = "Assistant claim (not execution evidence)"
					body = d.Part.Text
				}
			}
		case event.TypeCouncilVerdict, event.TypeCouncilDecided:
			label = "Previous council judgment (not execution evidence)"
			body = string(e.Data)
		case event.TypeTodosChanged:
			label = "Plan update"
			body = string(e.Data)
		}
		if strings.TrimSpace(body) == "" {
			continue
		}
		line := fmt.Sprintf("\n## %d %s\n%s\n", e.Seq, label, text.HeadTail(body, councilActionCap))
		entries = append(entries, line)
		size += len(line)
		for size > councilPrefixCap && len(entries) > 1 {
			dropped = true
			size -= len(entries[0])
			entries = entries[1:]
		}
	}
	if dropped {
		return header + omitted + strings.Join(entries, "")
	}
	return header + strings.Join(entries, "")
}

type councilPreparer interface {
	Prepare(context.Context, port.DeliberationRequest) error
}

// A single worker per turn coalesces notifications and is cancelled with the turn.
// It never generates, writes conversation events, or blocks appendFact.
type councilPreparation struct {
	wake   chan struct{}
	stop   context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
	closed bool
}

func (a *App) startCouncilPreparation(ctx context.Context, s session.Session) *councilPreparation {
	prep, ok := a.councilFor(s.ID).(councilPreparer)
	if !ok {
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	w := &councilPreparation{wake: make(chan struct{}, 1), stop: cancel, done: make(chan struct{})}
	a.mu.Lock()
	a.stateLocked(s.ID).councilPreparation = w
	a.mu.Unlock()
	go func() {
		defer close(w.done)
		var last string
		for {
			select {
			case <-ctx.Done():
				return
			case <-w.wake:
			}
			timer := time.NewTimer(100 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			evs, err := a.store.Read(ctx, s.ID, 0)
			if err != nil {
				continue
			}
			prefix := councilPrefix(a.taskEvents(s.ID, evs))
			if prefix == last {
				continue
			}
			members, rule := a.councilParams()
			bounded, stop := context.WithTimeout(ctx, 30*time.Second)
			started := time.Now()
			err = prep.Prepare(bounded, port.DeliberationRequest{EvidencePrefix: prefix, Members: members, Rule: rule, DefaultModel: a.sessionInfo(ctx, s.ID).Model.Model})
			stop()
			switch {
			case errors.Is(err, port.ErrCapabilityAbsent):
				last = prefix // examined but unsupported; no generated fallback
			case errors.Is(err, port.ErrPrefillBusy), errors.Is(err, context.Canceled) && ctx.Err() == nil:
				timer := time.NewTimer(250 * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
					w.notify()
				}
			case err != nil && ctx.Err() == nil:
				log.Printf("magi: council prefix preparation failed after %s: %v", time.Since(started).Round(time.Millisecond), err)
			case err == nil:
				last = prefix
			}
		}
	}()
	return w
}
func (w *councilPreparation) notify() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.closed {
		select {
		case w.wake <- struct{}{}:
		default:
		}
	}
}
func (w *councilPreparation) close() {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.closed = true
	w.stop()
	w.mu.Unlock()
	<-w.done
}

func (a *App) notifyCouncilPreparation(sid session.SessionID) {
	a.mu.Lock()
	st, ok := a.stateIf(sid)
	var w *councilPreparation
	if ok {
		w = st.councilPreparation
	}
	a.mu.Unlock()
	w.notify()
}
