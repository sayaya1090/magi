package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
)

// A resumed conversation keeps its times.
//
// The terminal stamps a block when the event arrives, so times were a property of having been
// watching. Reopen the same session tomorrow and every HH:MM was gone — the rebuild had nothing to
// stamp from, because the message it rebuilt did not carry the time the log had recorded.
func TestAResumedConversationKeepsItsTimes(t *testing.T) {
	at := time.Date(2026, 8, 11, 4, 5, 0, 0, time.UTC)
	blocks := rebuildBlocks([]session.Message{
		{ID: "m1", Role: session.RoleUser, At: at,
			Parts: []session.Part{{Kind: session.PartText, Text: "go and do it"}}},
		{ID: "m2", Role: session.RoleAssistant, At: at.Add(2 * time.Minute),
			Parts: []session.Part{{Kind: session.PartText, Text: "done"}}},
		{ID: "m3", Role: session.RoleUser,
			Parts: []session.Part{{Kind: session.PartText, Text: "from an older log"}}},
	})
	if len(blocks) != 3 {
		t.Fatalf("rebuilt %d blocks", len(blocks))
	}
	if !blocks[0].ts.Equal(at) {
		t.Errorf("the prompt rebuilt with ts %v, want %v", blocks[0].ts, at)
	}
	if blocks[1].ts.Equal(blocks[0].ts) {
		t.Errorf("both blocks carry %v — the stamp is not the message's own", blocks[1].ts)
	}
	// And it reaches the screen, which is the only place it matters.
	m := newTestModel(t)
	m.width, m.height = 120, 40
	m.blocks = blocks
	// In the stamp's own zone, which is how the terminal writes it: an event is stamped where it
	// happened, and the person reading the terminal is on that machine. (The console converts,
	// because a browser can be anywhere.)
	if out := m.transcript(); !strings.Contains(out, at.Format("15:04")) {
		t.Errorf("the rebuilt transcript shows no time:\n%s", out)
	}
	// A message the log never stamped shows nothing rather than 1970.
	if !blocks[2].ts.IsZero() {
		t.Errorf("an unstamped message rebuilt with ts %v", blocks[2].ts)
	}
}
