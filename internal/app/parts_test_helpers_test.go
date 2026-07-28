package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// joinPartText concatenates a message's text parts, newline-separated — how a test reads back what
// actually reached the conversation.
func joinPartText(parts []session.Part) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Kind == session.PartText {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// sessionText concatenates every prompt that reached a session — what magi actually said to the
// agent, as opposed to what a caller intended to say.
func sessionText(t *testing.T, a *App, sid session.SessionID) string {
	t.Helper()
	evs, err := a.store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, e := range evs {
		if e.Type != event.TypePromptSubmitted {
			continue
		}
		var d event.PromptSubmittedData
		if json.Unmarshal(e.Data, &d) == nil {
			b.WriteString(joinPartText(d.Parts))
			b.WriteString("\n")
		}
	}
	return b.String()
}
