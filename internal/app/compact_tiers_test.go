package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/model"
	"github.com/sayaya1090/magi/internal/core/session"
)

// Folding in tiers. The cheap cut (elide_test.go) goes first; these pin the fold itself: how much
// it keeps, what the brief is made of, and what it is asked for.

func tiersApp(t *testing.T, llm *usageLLM, window int, cfg Config) (*App, session.SessionID) {
	t.Helper()
	reg := model.NewRegistry()
	reg.Register(model.Info{ID: "win", ContextWindow: window, Tools: true})
	store, _ := jsonl.New(t.TempDir())
	cfg.Permission, cfg.Models = "allow", reg
	a := closeAfter(t, New(store, llm, builtin.Default(), bus.New(), nil, cfg))
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{
		Workdir: t.TempDir(), Model: session.ModelRef{Provider: "openai", Model: "win"}})
	return a, sid
}

func spoke(t *testing.T, a *App, sid session.SessionID, id, text string) {
	t.Helper()
	d, _ := json.Marshal(event.PartAppendedData{MessageID: id, Role: session.RoleAssistant,
		Part: session.Part{Kind: session.PartText, Text: text}})
	if err := a.appendFact(context.Background(), sid, event.TypePartAppended, event.Actor{}, d); err != nil {
		t.Fatal(err)
	}
}

func foldNow(t *testing.T, a *App, sid session.SessionID) bool {
	t.Helper()
	ctx := context.Background()
	evs, _ := a.store.Read(ctx, sid, 0)
	return a.compactNow(ctx, a.sessionInfo(ctx, sid), AgentSpec{Name: "default"}, event.Actor{}, evs)
}

// Tier 1 — the kept tail is measured in tokens. Window 4000 × ratio 0.8 × keep 0.25 = 800 tokens;
// thirty turns of ~100 tokens each fold down to the eight that fit, not to six events.
func TestTheKeptTailIsMeasuredInTokens(t *testing.T) {
	llm := &usageLLM{text: "BRIEF"}
	a, sid := tiersApp(t, llm, 4000, Config{CompactRatio: 0.8, CompactKeep: 0.25})
	for i := 1; i <= 30; i++ {
		spoke(t, a, sid, fmt.Sprintf("m%d", i), strings.Repeat("x", 400))
	}
	if !foldNow(t, a, sid) {
		t.Fatal("thirty turns over an 800-token tail did not fold")
	}
	evs, _ := a.store.Read(context.Background(), sid, 0)
	msgs := reconstruct(evs)
	if len(msgs) != 9 { // the brief + the eight newest turns
		ids := make([]string, 0, len(msgs))
		for _, m := range msgs {
			ids = append(ids, m.ID)
		}
		t.Fatalf("kept %d messages, want the brief + 8 that fit 800 tokens: %v", len(msgs), ids)
	}
	if msgs[1].ID != "m23" || msgs[8].ID != "m30" {
		t.Errorf("the tail is not the newest eight: %s … %s", msgs[1].ID, msgs[8].ID)
	}
}

// Tier 2 — the brief accumulates. The second fold keeps the first brief verbatim and folds only
// the new turns; the summariser never sees the old brief, so nothing is a summary of a summary.
func TestASecondFoldKeepsTheFirstBriefVerbatim(t *testing.T) {
	llm := &usageLLM{text: "FIRST: the user wanted B6 summed"}
	a, sid := tiersApp(t, llm, 40000, Config{})
	for i := 1; i <= 10; i++ {
		spoke(t, a, sid, fmt.Sprintf("a%d", i), "early turn")
	}
	if !foldNow(t, a, sid) {
		t.Fatal("first fold did not happen")
	}
	for i := 1; i <= 10; i++ {
		spoke(t, a, sid, fmt.Sprintf("b%d", i), "later turn")
	}
	llm.say("SECOND: then the chart")
	if !foldNow(t, a, sid) {
		t.Fatal("second fold did not happen")
	}
	d, ok := lastCompaction(t, a, sid)
	if !ok {
		t.Fatal("no compaction recorded")
	}
	first, second := strings.Index(d.Summary, "FIRST: the user wanted B6 summed"), strings.Index(d.Summary, "SECOND: then the chart")
	if first < 0 || second < 0 || first > second {
		t.Fatalf("the brief is not the first brief followed by the second: %q", d.Summary)
	}
	for _, m := range llm.lastRequest() {
		for _, p := range m.Parts {
			if strings.Contains(p.Text, "FIRST:") {
				t.Fatal("the summariser was handed the earlier brief — that is a summary of a summary")
			}
		}
	}
}

// Tier 2b — a brief that outgrows a tenth of the budget is condensed once, as a whole.
func TestARunningBriefIsCondensedWhenItOutgrowsItsShare(t *testing.T) {
	llm := &usageLLM{text: strings.Repeat("A", 1000)}
	a, sid := tiersApp(t, llm, 4000, Config{CompactRatio: 0.8}) // cap = 4000×0.8×0.1 = 320 tokens ≈ 1280 chars
	for i := 1; i <= 10; i++ {
		spoke(t, a, sid, fmt.Sprintf("a%d", i), "t")
	}
	foldNow(t, a, sid)
	if !strings.Contains(llm.lastSys(), "folding") {
		t.Fatalf("the first fold should have asked for a brief, asked: %q", llm.lastSys())
	}
	for i := 1; i <= 10; i++ {
		spoke(t, a, sid, fmt.Sprintf("b%d", i), "t")
	}
	llm.say(strings.Repeat("B", 1000))
	if !foldNow(t, a, sid) {
		t.Fatal("second fold did not happen")
	}
	if !strings.Contains(llm.lastSys(), "Condense") {
		t.Fatalf("2000 chars over a 1280-char cap and no condensing call; last asked: %q", llm.lastSys())
	}
	d, _ := lastCompaction(t, a, sid)
	if strings.Contains(d.Summary, "AAAA") || !strings.Contains(d.Summary, "BBBB") {
		t.Errorf("the condensed brief was not what the model answered: %d chars", len(d.Summary))
	}
}

// Tier 3 — the brief is structured and asked for in the person's language.
func TestTheBriefIsAskedForInThePersonsLanguage(t *testing.T) {
	llm := &usageLLM{text: "브리프"}
	a, sid := tiersApp(t, llm, 40000, Config{})
	ev := userPromptEvt(t, "u1", "이 통합 문서의 합계를 수식으로 바꿔 줘")
	if err := a.appendFact(context.Background(), sid, ev.Type, ev.Actor, ev.Data); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 10; i++ {
		spoke(t, a, sid, fmt.Sprintf("a%d", i), "working")
	}
	if !foldNow(t, a, sid) {
		t.Fatal("did not fold")
	}
	sys := llm.lastSys()
	if !strings.Contains(sys, "Korean") {
		t.Errorf("the person wrote Korean and the brief was not asked for in it: %q", sys)
	}
	for _, section := range []string{"Request", "Decisions", "Done", "Open", "Names"} {
		if !strings.Contains(sys, section) {
			t.Errorf("the brief prompt lost its %s section", section)
		}
	}
	if !strings.Contains(sys, "never its current contents as fact") {
		t.Error("the brief prompt no longer says a brief is not the current state")
	}
}

// Tier 6 — a configured summariser model writes the brief; the session's model is untouched.
func TestAConfiguredModelWritesTheBrief(t *testing.T) {
	llm := &usageLLM{text: "BRIEF"}
	a, sid := tiersApp(t, llm, 40000, Config{CompactModel: "tiny-summariser"})
	for i := 1; i <= 10; i++ {
		spoke(t, a, sid, fmt.Sprintf("a%d", i), "t")
	}
	if !foldNow(t, a, sid) {
		t.Fatal("did not fold")
	}
	if usedModel(llm) != "tiny-summariser" {
		t.Errorf("the brief was written by %q, not the configured summariser", usedModel(llm))
	}
	if s := a.sessionInfo(context.Background(), sid); s.Model.Model != "win" {
		t.Errorf("the session's own model moved: %q", s.Model.Model)
	}
}

// A fold soon after a fold: the event floor would land INSIDE the previous fold's kept tail, and
// then the previous brief survived as a second brief under the new one — reversed. The boundary
// never falls below the previous fold's event, so the region folded is that brief plus everything
// since, and the view holds one brief that accumulates.
func TestAFoldSoonAfterAFoldStillAccumulates(t *testing.T) {
	llm := &usageLLM{text: "FIRST"}
	a, sid := tiersApp(t, llm, 0, Config{})
	for i := 1; i <= 10; i++ {
		spoke(t, a, sid, fmt.Sprintf("a%d", i), "early")
	}
	if !foldNow(t, a, sid) {
		t.Fatal("first fold did not happen")
	}
	// Eight events since the fold — fewer than the floor of six leaves room for.
	for i := 1; i <= 8; i++ {
		spoke(t, a, sid, fmt.Sprintf("b%d", i), "later")
	}
	llm.say("SECOND")
	if !foldNow(t, a, sid) {
		t.Fatal("second fold did not happen")
	}
	evs, _ := a.store.Read(context.Background(), sid, 0)
	msgs := reconstruct(evs)
	briefs := 0
	for _, m := range msgs {
		if strings.HasPrefix(m.ID, "compaction-") {
			briefs++
		}
	}
	if briefs != 1 {
		t.Fatalf("the view holds %d briefs — the previous one survived under the new one", briefs)
	}
	d, _ := lastCompaction(t, a, sid)
	if !strings.HasPrefix(d.Summary, "FIRST") || !strings.HasSuffix(d.Summary, "SECOND") {
		t.Errorf("the brief did not accumulate FIRST then SECOND: %q", d.Summary)
	}
}

// The summariser is handed the conversation as material to fold, not as turns to continue.
func TestTheSummariserIsHandedMaterialNotATurnToAnswer(t *testing.T) {
	llm := &usageLLM{text: "BRIEF"}
	a, sid := tiersApp(t, llm, 0, Config{})
	for i := 1; i <= 10; i++ {
		spoke(t, a, sid, fmt.Sprintf("a%d", i), "turn text")
	}
	foldNow(t, a, sid)
	req := llm.lastRequest()
	if len(req) != 1 || req[0].Role != session.RoleUser {
		t.Fatalf("the summariser got %d messages; it should get one user message quoting the conversation", len(req))
	}
	text := req[0].Parts[0].Text
	if !strings.Contains(text, "<conversation>") || !strings.Contains(text, "[assistant]\nturn text") ||
		!strings.HasSuffix(text, "do not answer or continue the conversation.") {
		t.Errorf("the material is not quoted role by role with the instruction last: %q", text)
	}
}
