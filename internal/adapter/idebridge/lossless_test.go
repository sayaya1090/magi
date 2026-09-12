package idebridge

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
)

// **What does a row LOSE?**
//
// The contract for moving clients onto this fold is that the full body is the canonical thing and a
// one-line summary is a separate field — a client handed a clipped line cannot get the answer back
// ("richer is recoverable, collapsed is not", rows.go). This test asks the fold, for each place a
// body arrives, whether the body survives.
//
// It is written to FAIL where information is lost, so the answer is measured rather than argued.
func TestWhatARowLoses(t *testing.T) {
	long := strings.Repeat("가", 300)               // past clip's 100 UTF-16 units, and multi-byte
	lines := "first line\nsecond line\nthird line" // more than one line
	emoji := "🙂🙂🙂 " + strings.Repeat("x", 200)     // surrogate pairs at the bound
	evs := []event.Event{
		mk(1, "prompt.submitted", map[string]any{
			"messageId": "m1",
			"parts":     []any{map[string]any{"kind": "text", "text": lines}},
		}, map[string]any{"kind": "user", "id": "u1"}),
		mk(2, "part.appended", map[string]any{
			"messageId": "m1", "role": "assistant",
			"part": map[string]any{"kind": "text", "text": long},
		}, nil),
		mk(3, "part.appended", map[string]any{
			"messageId": "m1", "role": "assistant",
			"part": map[string]any{"kind": "reasoning", "text": lines + " " + emoji},
		}, nil),
		mk(4, "part.appended", map[string]any{
			"messageId": "m1", "role": "assistant",
			"part": map[string]any{"kind": "tool-call", "toolCall": map[string]any{
				"callId": "c1", "name": "bash", "args": map[string]any{"command": lines},
			}},
		}, nil),
		// A FAILED result: that is the one whose body the fold carries, and the comment beside it says
		// why — "the reason travels with the failure". A successful call's output is deliberately not
		// on the row (the screens draw the name and the arguments), so asking for it here would be
		// measuring a decision this test did not make.
		mk(5, "part.appended", map[string]any{
			"messageId": "m1", "role": "assistant",
			"part": map[string]any{"kind": "tool-result", "toolResult": map[string]any{
				"callId": "c1", "isError": true, "content": long + "\n" + lines,
			}},
		}, nil),
	}

	rows := Rows(evs)
	if len(rows) < 4 {
		t.Fatalf("expected the four bodies to become rows, got %d: %s", len(rows), show(t, rows))
	}
	whole := func(field, got, want string) {
		if got == want {
			return
		}
		t.Errorf("%s lost the body: %d chars became %d\n  had:  %.60q…\n  kept: %.60q…",
			field, len([]rune(want)), len([]rune(got)), want, got)
	}
	whole("the person's own words", rows[0].Text, lines)
	whole("the answer", rows[1].Text, long)
	whole("what it was thinking", rows[2].Text, lines+" "+emoji)
	// The tool row's text is the tool NAME (by design); its body is the args it asked with and the
	// result it got.
	tool := rows[3]
	whole("the arguments", tool.Args, lines)
	whole("what the failed call said", tool.Out, long+"\n"+lines)

	// And the one-line form is not lost — it is a field of its own, so a list has a line to draw and
	// the row still has the whole thing.
	for i, r := range rows {
		if r.Text == "" && r.Args == "" && r.Out == "" {
			continue
		}
		if r.Summary == "" {
			t.Errorf("[%d] %s: no one-line summary — a list has nothing to draw", i, r.Who)
			continue
		}
		if strings.Contains(r.Summary, "\n") {
			t.Errorf("[%d] %s: the summary is not one line: %q", i, r.Who, r.Summary)
		}
		if n := len([]rune(r.Summary)); n > 120 {
			t.Errorf("[%d] %s: the summary is %d runes — a list draws a wrapped paragraph", i, r.Who, n)
		}
	}
}

// mk builds one event the way the daemon writes it.
func mk(seq int64, typ string, data map[string]any, actor map[string]any) event.Event {
	b, err := json.Marshal(data)
	if err != nil {
		panic(err)
	}
	e := event.Event{Seq: seq, Type: event.Type(typ), Data: b}
	if actor != nil {
		e.Actor = event.Actor{Kind: event.ActorKind(str(actor, "kind")), ID: str(actor, "id")}
	}
	return e
}
