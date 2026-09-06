package jsonl

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A session's meta must report the model it is on NOW, not the one it opened with. The console
// reads this: with only session.created to go on, its model menu repainted the opening model after
// every successful change, so a switch that had actually landed looked like it had been refused.
//
// Labels are read the same way in the same walk, and the walk has to collect BOTH — stopping at
// whichever came last would leave the other at its opening value.
func TestSessionMetaCarriesTheModelItIsOnNow(t *testing.T) {
	dir := t.TempDir()
	st, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	sid := session.SessionID("s_model")
	wd := t.TempDir()

	created, _ := json.Marshal(event.SessionCreatedData{
		Workdir: wd, Model: session.ModelRef{Provider: "openai", Model: "opus"},
	})
	labels, _ := json.Marshal(event.LabelsChangedData{Labels: []string{"billing"}})
	changed, _ := json.Marshal(event.ModelChangedData{Model: "haiku"})
	if _, err := st.Append(ctx, sid,
		event.Event{SessionID: sid, Type: event.TypeSessionCreated, Data: created},
		event.Event{SessionID: sid, Type: event.TypeModelChanged, Data: changed},
		event.Event{SessionID: sid, Type: event.TypeLabelsChanged, Data: labels},
	); err != nil {
		t.Fatal(err)
	}

	metas, err := st.ListSessions(ctx, wd)
	if err != nil {
		t.Fatal(err)
	}
	var got *session.SessionMeta
	for i := range metas {
		if metas[i].ID == sid {
			got = &metas[i]
		}
	}
	if got == nil {
		t.Fatal("the session is not in its own workdir's list")
	}
	if got.Model != "haiku" {
		t.Errorf("the meta says the model is %q; it was changed to haiku", got.Model)
	}
	if len(got.Labels) != 1 || got.Labels[0] != "billing" {
		t.Errorf("the walk lost the labels while reading the model: %v", got.Labels)
	}
}

// And with nothing changed, the opening model is still the answer.
func TestAnUnchangedSessionReportsWhatItOpenedWith(t *testing.T) {
	dir := t.TempDir()
	st, _ := New(dir)
	ctx := context.Background()
	wd := t.TempDir()
	created, _ := json.Marshal(event.SessionCreatedData{
		Workdir: wd, Model: session.ModelRef{Provider: "openai", Model: "sonnet"},
	})
	st.Append(ctx, "s_plain", event.Event{SessionID: "s_plain", Type: event.TypeSessionCreated, Data: created})
	metas, _ := st.ListSessions(ctx, wd)
	if len(metas) != 1 || metas[0].Model != "sonnet" {
		t.Fatalf("the opening model came back as %v", metas)
	}
}

// What a session was opened FOR rides its created event and comes back in the listing. The Office
// helper keys its documents on this: after a restart it has no bindings of its own, and without
// this field the only move was a fresh, empty conversation for a document that already had one.
func TestSessionMetaCarriesWhatItWasOpenedFor(t *testing.T) {
	dir := t.TempDir()
	st, _ := New(dir)
	ctx := context.Background()
	wd := t.TempDir()
	created, _ := json.Marshal(event.SessionCreatedData{Workdir: wd, For: "wb-book-7"})
	st.Append(ctx, "s_for", event.Event{SessionID: "s_for", Type: event.TypeSessionCreated, Data: created})
	metas, _ := st.ListSessions(ctx, wd)
	if len(metas) != 1 || metas[0].For != "wb-book-7" {
		t.Fatalf("the listing lost what the session was opened for: %v", metas)
	}
}
