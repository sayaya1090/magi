package app

import (
	"encoding/json"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"strings"
	"testing"
)

func prefixPart(seq int64, p session.Part) event.Event {
	d, _ := json.Marshal(event.PartAppendedData{Role: session.RoleAssistant, Part: p})
	return event.Event{Seq: seq, Type: event.TypePartAppended, Data: d}
}
func TestCouncilPrefixPreservesHistoryAndJudgment(t *testing.T) {
	a := []event.Event{prefixPart(1, session.Part{Kind: session.PartText, Text: "initial claim"})}
	first := councilPrefix(a)
	a = append(a, event.Event{Seq: 2, Type: event.TypeCouncilDecided, Data: json.RawMessage(`{"decision":"continue","feedback":"fix the empty input"}`)})
	a = append(a, prefixPart(3, session.Part{Kind: session.PartText, Text: "fixed empty input"}))
	next := councilPrefix(a)
	if !strings.HasPrefix(next, first) {
		t.Fatal("appended judgment and fixes rewrote the prefix")
	}
	if !strings.Contains(next, "not execution evidence") || !strings.Contains(next, "fix the empty input") {
		t.Fatal(next)
	}
	if strings.Index(next, "fix the empty input") > strings.Index(next, "fixed empty input") {
		t.Fatal("judgment must precede the fix")
	}
	a = append(a, event.Event{Seq: 4, Type: event.TypeTurnFinished}, prefixPart(5, session.Part{Kind: session.PartText, Text: "new turn"}))
	if strings.Contains(councilPrefix(a), "initial claim") {
		t.Fatal("previous turn leaked")
	}
}

func TestReportReadyCannotUsePreviousTurnText(t *testing.T) {
	evs := []event.Event{prefixPart(1, session.Part{Kind: session.PartText, Text: "old final report"}), {Seq: 2, Type: event.TypeTurnFinished}}
	if currentTurnAssistantText(evs) != "" {
		t.Fatal("old report treated as current")
	}
	evs = append(evs, prefixPart(3, session.Part{Kind: session.PartText, Text: "new short report"}))
	if currentTurnAssistantText(evs) != "new short report" {
		t.Fatal("short current report lost")
	}
}
