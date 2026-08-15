package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/config"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// LookOver reviews the region a person is editing and answers per line: the console sends the lines
// around the caret numbered with their real line numbers, and the system prompt demands
// `<line><TAB><clause>` back, so each remark can be drawn against the line it names rather than as a
// paragraph over the file.
func TestLookOverAsksForLineAnchoredFindings(t *testing.T) {
	cap := &acCapLLM{text: "42\tthis error is swallowed"}
	a, sid := completeApp(t, cap, config.AutocompleteConfig{}, nil, nil)
	got, err := a.LookOver(context.Background(), sid, "x.go", "42\tif err != nil {\n43\t}\n")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(got) == "" {
		t.Fatal("no answer came back")
	}
	sys := cap.request().System
	for _, want := range []string{"line number and a tab", "<line-number>", "region around"} {
		if !strings.Contains(sys, want) {
			t.Errorf("the system prompt does not ask for line-anchored findings (missing %q):\n%s", want, sys)
		}
	}
	// The numbered region travels verbatim as the user message.
	if msgs := cap.request().Messages; len(msgs) == 0 || len(msgs[0].Parts) == 0 ||
		!strings.Contains(msgs[0].Parts[0].Text, "42\tif err") {
		t.Errorf("the numbered region did not reach the model: %+v", cap.request().Messages)
	}
}

// The remark comes back in the reader's language: LookOver detects it from the session's genuine
// user prompts and prepends the same "reply in <language>" directive the turn loop uses — first, so
// a weak model cannot drift back to English past a rule buried further down.
func TestLookOverRepliesInTheReadersLanguage(t *testing.T) {
	cap := &acCapLLM{text: "3\t널 포인터일 수 있습니다"}
	a, sid := completeApp(t, cap, config.AutocompleteConfig{}, nil, nil)
	data, err := json.Marshal(event.PromptSubmittedData{
		Parts: []session.Part{{Kind: session.PartText, Text: "이 파일 좀 검토해줘"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.Append(context.Background(), sid, event.Event{
		Type:  event.TypePromptSubmitted,
		Actor: event.Actor{Kind: event.ActorUser, ID: "test"},
		Data:  data,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.LookOver(context.Background(), sid, "x.go", "3\tp := foo()\n"); err != nil {
		t.Fatal(err)
	}
	sys := cap.request().System
	ko, edit := strings.Index(sys, "Korean"), strings.Index(sys, "editing")
	if ko < 0 {
		t.Fatalf("no Korean language directive in the system prompt:\n%s", sys)
	}
	if edit >= 0 && ko > edit {
		t.Errorf("the language directive is not ahead of the review prompt:\n%s", sys)
	}
}

// A look-over can fire before the person has sent any chat prompt (they opened a file and started
// typing). There is no genuine prompt to read the language from yet, so the "English" result must
// NOT be cached — otherwise a later Korean prompt would never change the review's language. This
// pins the cache to only commit once a real prompt exists.
func TestLookOverDoesNotLockEnglishBeforeAnyPrompt(t *testing.T) {
	cap := &acCapLLM{text: ""}
	a, sid := completeApp(t, cap, config.AutocompleteConfig{}, nil, nil)
	// First look-over: the session has no genuine user prompt yet.
	if _, err := a.LookOver(context.Background(), sid, "x.go", "1\tvar x int\n"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cap.request().System, "# Language") {
		t.Fatalf("a directive appeared before any prompt existed:\n%s", cap.request().System)
	}
	// Now the person sends a Korean prompt.
	data, err := json.Marshal(event.PromptSubmittedData{
		Parts: []session.Part{{Kind: session.PartText, Text: "이 파일 검토해줘"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.Append(context.Background(), sid, event.Event{
		Type:  event.TypePromptSubmitted,
		Actor: event.Actor{Kind: event.ActorUser, ID: "test"},
		Data:  data,
	}); err != nil {
		t.Fatal(err)
	}
	// The next look-over must pick up Korean — the earlier empty result was not cached.
	if _, err := a.LookOver(context.Background(), sid, "x.go", "1\tvar x int\n"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cap.request().System, "Korean") {
		t.Errorf("the language did not update after the first genuine prompt (English was cached):\n%s",
			cap.request().System)
	}
}

// English (or any Latin script) leaves the prompt as it was — no directive, no wasted primacy line.
func TestLookOverAddsNoDirectiveForEnglish(t *testing.T) {
	cap := &acCapLLM{text: ""}
	a, sid := completeApp(t, cap, config.AutocompleteConfig{}, nil, nil)
	data, _ := json.Marshal(event.PromptSubmittedData{
		Parts: []session.Part{{Kind: session.PartText, Text: "take a look at this file"}}})
	if _, err := a.store.Append(context.Background(), sid, event.Event{
		Type:  event.TypePromptSubmitted,
		Actor: event.Actor{Kind: event.ActorUser, ID: "test"},
		Data:  data,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.LookOver(context.Background(), sid, "x.go", "1\tvar x int\n"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cap.request().System, "# Language") {
		t.Errorf("an English session got a language directive it did not need:\n%s", cap.request().System)
	}
}
