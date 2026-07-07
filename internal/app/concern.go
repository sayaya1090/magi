package app

import (
	"context"
	"encoding/json"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// maxOpenConcerns bounds the ledger view so a long session cannot flood the council's
// evidence with stale structural signals. The most-recent open concerns win (a fresh
// raise for a Key moves it to the front), matching how the council already prioritizes
// the latest turn's evidence.
const maxOpenConcerns = 12

// Concern is one entry of the folded ledger view: a durable, role-scoped structural
// signal that survived to the current point of the event log (raised and not since
// resolved). It mirrors port.Signal plus the Key/Scope that make it dedup-able and
// attributable, so a caller can hand it to the council as evidence directly.
type Concern struct {
	Key    string
	Source string
	Kind   string
	Status string
	Detail string
	Scope  string
}

// Signal renders a folded concern as council evidence, identical in shape to the
// signals the gate computes fresh each round — the point of the ledger is that a
// concern raised earlier (or in a child session) now arrives through the same channel.
func (c Concern) Signal() port.Signal {
	return port.Signal{Source: c.Source, Kind: c.Kind, Status: c.Status, Detail: c.Detail}
}

// sessionConcerns folds the event log into the live ledger: replay in seq order,
// TypeConcernRaised opens a Key, TypeConcernResolved closes it, and a later Raised for
// a closed Key REOPENS it. That reopen is the self-healing property the reset safety
// rests on — a deterministic producer that re-raises a still-true concern each turn
// undoes any prior resolve, so a guarded orchestrator reset can only clear advisory
// memory, never suppress a fact that is still true.
//
// Returns the open concerns most-recent-first, deduped by Key, capped to
// maxOpenConcerns. Pure function of the events passed — fully testable without a model,
// and recomputed each turn so nothing has to be held in mutable state.
func sessionConcerns(evs []event.Event) []Concern {
	// order preserves first-seen raise order among currently-open keys; a re-raise of an
	// already-open key refreshes its payload but keeps its position, while a raise that
	// REOPENS a resolved key appends it as newest.
	open := make(map[string]Concern)
	var order []string

	appendKey := func(k string) {
		for _, e := range order {
			if e == k {
				return
			}
		}
		order = append(order, k)
	}
	removeKey := func(k string) {
		for i, e := range order {
			if e == k {
				order = append(order[:i], order[i+1:]...)
				return
			}
		}
	}

	for _, e := range evs {
		switch e.Type {
		case event.TypeConcernRaised:
			var d event.ConcernRaisedData
			if json.Unmarshal(e.Data, &d) != nil || d.Key == "" {
				continue
			}
			_, wasOpen := open[d.Key]
			open[d.Key] = Concern{
				Key: d.Key, Source: d.Source, Kind: d.Kind,
				Status: d.Status, Detail: d.Detail, Scope: d.Scope,
			}
			if !wasOpen {
				// newly opened (or reopened after a resolve) → newest position
				appendKey(d.Key)
			}
		case event.TypeConcernResolved:
			var d event.ConcernResolvedData
			if json.Unmarshal(e.Data, &d) != nil || d.Key == "" {
				continue
			}
			delete(open, d.Key)
			removeKey(d.Key)
		}
	}

	// most-recent-first, capped.
	out := make([]Concern, 0, len(order))
	for i := len(order) - 1; i >= 0; i-- {
		c, ok := open[order[i]]
		if !ok {
			continue
		}
		out = append(out, c)
		if len(out) >= maxOpenConcerns {
			break
		}
	}
	return out
}

// appendConcernRaised persists a concern.raised fact. Callers guard idempotency by only
// raising when the Key is not already open (the fold dedups regardless, but not
// re-appending an identical fact every council round keeps the log from growing without
// bound). Low-level; the producers (council gate, subagent boundary) supply the Key/Scope.
func (a *App) appendConcernRaised(ctx context.Context, sid session.SessionID, actor event.Actor, key, source, kind, status, detail, scope string) error {
	d, _ := json.Marshal(event.ConcernRaisedData{
		Key: key, Source: source, Kind: kind, Status: status, Detail: detail, Scope: scope,
	})
	return a.appendFact(ctx, sid, event.TypeConcernRaised, actor, d)
}

// appendConcernResolved persists a concern.resolved tombstone by Key. By is "auto" for a
// deterministic recovery or "orchestrator" for a guarded judged reset. The raised fact is
// never deleted; the fold simply treats the Key as closed until (if ever) re-raised.
func (a *App) appendConcernResolved(ctx context.Context, sid session.SessionID, actor event.Actor, key, by, reason string) error {
	d, _ := json.Marshal(event.ConcernResolvedData{Key: key, By: by, Reason: reason})
	return a.appendFact(ctx, sid, event.TypeConcernResolved, actor, d)
}
