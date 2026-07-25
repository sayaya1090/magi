package app

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
)

// evConcernRaised / evConcernResolved build the ledger fact events sessionConcerns folds.
func evConcernRaised(key, kind, detail, scope string) event.Event {
	d, _ := json.Marshal(event.ConcernRaisedData{
		Key: key, Source: "self-check", Kind: kind, Status: "fail", Detail: detail, Scope: scope,
	})
	return event.Event{Type: event.TypeConcernRaised, Data: d}
}

func evConcernResolved(key, by string) event.Event {
	d, _ := json.Marshal(event.ConcernResolvedData{Key: key, By: by, Reason: "test"})
	return event.Event{Type: event.TypeConcernResolved, Data: d}
}

func keysOf(cs []Concern) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Key
	}
	return out
}

func hasKey(cs []Concern, key string) bool {
	for _, c := range cs {
		if c.Key == key {
			return true
		}
	}
	return false
}

func TestSessionConcernsFold(t *testing.T) {
	t.Run("a lone raise is open", func(t *testing.T) {
		got := sessionConcerns([]event.Event{evConcernRaised("k1", "unverified-premise", "d", "")})
		if !hasKey(got, "k1") {
			t.Fatalf("k1 should be open, got %v", keysOf(got))
		}
	})

	t.Run("raise then resolve closes it", func(t *testing.T) {
		got := sessionConcerns([]event.Event{
			evConcernRaised("k1", "unverified-premise", "d", ""),
			evConcernResolved("k1", "auto"),
		})
		if hasKey(got, "k1") {
			t.Fatalf("k1 resolved, should be closed, got %v", keysOf(got))
		}
	})

	// self-healing: a re-raise after a resolve reopens — the property that makes an
	// orchestrator reset unable to bury a still-true fact.
	t.Run("resolve then raise again REOPENS (self-healing)", func(t *testing.T) {
		got := sessionConcerns([]event.Event{
			evConcernRaised("k1", "unverified-premise", "d", ""),
			evConcernResolved("k1", "orchestrator"),
			evConcernRaised("k1", "unverified-premise", "d", ""),
		})
		if !hasKey(got, "k1") {
			t.Fatalf("k1 re-raised after resolve should REOPEN, got %v", keysOf(got))
		}
	})

	t.Run("dedup: repeated raises of one Key yield one open concern with latest detail", func(t *testing.T) {
		got := sessionConcerns([]event.Event{
			evConcernRaised("k1", "unverified-premise", "old", ""),
			evConcernRaised("k1", "unverified-premise", "new", ""),
		})
		if len(got) != 1 {
			t.Fatalf("want 1 concern, got %d (%v)", len(got), keysOf(got))
		}
		if got[0].Detail != "new" {
			t.Fatalf("re-raise should refresh detail, got %q", got[0].Detail)
		}
	})

	t.Run("cap: at most maxOpenConcerns, most-recent-first", func(t *testing.T) {
		var evs []event.Event
		for i := 0; i < maxOpenConcerns+5; i++ {
			evs = append(evs, evConcernRaised(fmt.Sprintf("k%02d", i), "kind", "d", ""))
		}
		got := sessionConcerns(evs)
		if len(got) != maxOpenConcerns {
			t.Fatalf("want cap %d, got %d", maxOpenConcerns, len(got))
		}
		// most-recent-first: newest key must lead.
		newest := fmt.Sprintf("k%02d", maxOpenConcerns+4)
		if got[0].Key != newest {
			t.Fatalf("want newest %q first, got %q", newest, got[0].Key)
		}
	})

	// Positional semantics behind most-recent-first: a re-raise of an ALREADY-OPEN key keeps its
	// original position, but a raise that REOPENS a resolved key moves it to newest.
	t.Run("re-raise of an open key keeps its position", func(t *testing.T) {
		got := sessionConcerns([]event.Event{
			evConcernRaised("k1", "kind", "d", ""),
			evConcernRaised("k2", "kind", "d", ""),
			evConcernRaised("k1", "kind", "d2", ""), // re-raise of still-open k1 → position unchanged
		})
		// most-recent-first: k2 (raised last among distinct opens) leads, k1 keeps its older slot.
		if len(got) != 2 || got[0].Key != "k2" || got[1].Key != "k1" {
			t.Fatalf("re-raise of open k1 must keep its position (want [k2 k1]), got %v", keysOf(got))
		}
	})

	t.Run("reopen after resolve moves the key to newest", func(t *testing.T) {
		got := sessionConcerns([]event.Event{
			evConcernRaised("k1", "kind", "d", ""),
			evConcernRaised("k2", "kind", "d", ""),
			evConcernResolved("k1", "orchestrator"),
			evConcernRaised("k1", "kind", "d", ""), // REOPEN k1 → newest position
		})
		// k1, reopened last, must now lead the most-recent-first output.
		if len(got) != 2 || got[0].Key != "k1" || got[1].Key != "k2" {
			t.Fatalf("reopened k1 must move to newest (want [k1 k2]), got %v", keysOf(got))
		}
	})

	t.Run("malformed / empty-key events are ignored", func(t *testing.T) {
		got := sessionConcerns([]event.Event{
			{Type: event.TypeConcernRaised, Data: []byte("{bad json")},
			evConcernRaised("", "kind", "d", ""),
			evConcernRaised("k1", "kind", "d", ""),
		})
		if len(got) != 1 || got[0].Key != "k1" {
			t.Fatalf("only k1 should survive, got %v", keysOf(got))
		}
	})

	t.Run("scope is preserved (subagent attribution)", func(t *testing.T) {
		got := sessionConcerns([]event.Event{
			evConcernRaised("k1", "unverified-premise", "d", "subagent:scout"),
		})
		if len(got) != 1 || got[0].Scope != "subagent:scout" {
			t.Fatalf("scope should survive fold, got %+v", got)
		}
	})
}
