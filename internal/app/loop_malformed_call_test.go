package app

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// repairAskLLM is fakeLLM that also keeps every request it was sent, so a test can read what the
// model heard between two of its replies.
type repairAskLLM struct {
	fakeLLM
	rmu  sync.Mutex
	reqs []port.ChatRequest
}

func (r *repairAskLLM) StreamChat(ctx context.Context, req port.ChatRequest) (<-chan port.ProviderEvent, error) {
	r.rmu.Lock()
	r.reqs = append(r.reqs, req)
	r.rmu.Unlock()
	return r.fakeLLM.StreamChat(ctx, req)
}

func malformedStep(text string) []port.ProviderEvent {
	return []port.ProviderEvent{{Type: port.ProviderText, Text: text}, {Type: port.ProviderFinish, FinishReason: "stop", MalformedCall: true}}
}

func misnamedStep(text, name string) []port.ProviderEvent {
	return []port.ProviderEvent{{Type: port.ProviderText, Text: text}, {Type: port.ProviderFinish, FinishReason: "stop", MalformedCall: true, MalformedName: name}}
}

func lastRequestSays(r *repairAskLLM, i int, needle string) bool {
	r.rmu.Lock()
	defer r.rmu.Unlock()
	if i >= len(r.reqs) {
		return false
	}
	for _, m := range r.reqs[i].Messages {
		for _, p := range m.Parts {
			if strings.Contains(p.Text, needle) {
				return true
			}
		}
	}
	return false
}

// The reply shaped like a tool call is not shown as an answer: the model is asked to say it again
// as a real call, with its own reply quoted so it can correct rather than re-derive. The next reply
// is the answer. Guessing the tool from the argument keys was rejected (2026-09-07): telling the
// model it was wrong is right, and it stays right as tools are added.
// SPEC F-LLM-FALLBACK fallback-4: an object with no tool name is sent back for repair, quoted.
func TestAMalformedToolCallIsSentBackForRepair(t *testing.T) {
	llm := &repairAskLLM{fakeLLM: fakeLLM{steps: [][]port.ProviderEvent{
		malformedStep(`{"address":"A1","text":"사용자 메모"}`),
		toolStep("read", `{"path":"x"}`),
		textStep("붙였습니다"),
	}}}
	a, wd := newApp(t, llm, Config{Permission: "allow"})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	a.Submit(context.Background(), command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "A1 에 메모"}},
		Actor: event.Actor{Kind: event.ActorUser, ID: "test"}})
	got := waitForTerminal(t, a, sid)
	if countType(got, event.TypeTurnFinished) != 1 {
		t.Fatalf("expected one finished turn, got %v", typesOf(got))
	}
	if llm.call < 3 {
		t.Fatalf("the model should have been asked again after the malformed reply (calls=%d)", llm.call)
	}
	// The second request carries the repair request and the quoted reply.
	if !lastRequestSays(llm, 1, "SHAPED like a tool call") || !lastRequestSays(llm, 1, `{"address":"A1","text":"사용자 메모"}`) {
		t.Error("the second request must tell the model its reply was shaped like a call with no name, and quote it")
	}
	// The malformed JSON is not the answer the person sees.
	for _, e := range got {
		if e.Type == event.TypePartAppended && strings.Contains(string(e.Data), "A1") && strings.Contains(string(e.Data), "사용자 메모") {
			t.Errorf("the malformed reply was persisted as an answer: %s", e.Data)
		}
	}
	if !lastRequestSays(llm, 1, "Nothing ran") {
		t.Error("the repair request must say nothing ran")
	}
}

// Three repair requests at most; the fourth such reply is shown as text so the turn cannot loop.
// A reply that names something that is not a tool is told exactly that, with the name.
func TestMalformedToolCallRepairIsAskedThriceThenGivenUp(t *testing.T) {
	llm := &repairAskLLM{fakeLLM: fakeLLM{steps: [][]port.ProviderEvent{
		malformedStep(`{"address":"A1"}`),
		misnamedStep(`{"name":"sheet-design"}`, "sheet-design"),
		misnamedStep(`{"name":"excel_add_comment","arguments":{"address":"A1"}}`, "excel_add_comment"),
		malformedStep(`{"address":"A1"}`),
		textStep("never reached"),
	}}}
	a, wd := newApp(t, llm, Config{Permission: "allow"})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	a.Submit(context.Background(), command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "A1 에 메모"}},
		Actor: event.Actor{Kind: event.ActorUser, ID: "test"}})
	got := waitForTerminal(t, a, sid)
	if countType(got, event.TypeTurnFinished) != 1 {
		t.Fatalf("expected one finished turn, got %v", typesOf(got))
	}
	if llm.call != 4 {
		t.Errorf("three repairs then text: the model should have been called exactly 4 times, got %d", llm.call)
	}
	if !lastRequestSays(llm, 2, `named "sheet-design", which is NOT a tool`) {
		t.Error("a reply that named a non-tool is told the name is not a tool")
	}
	if !lastRequestSays(llm, 3, "LAST repair request") || !lastRequestSays(llm, 3, `"excel_add_comment"`) {
		t.Error("the third repair must say it is the last and name the wrong tool")
	}
}

func TestMalformedCallNudgeQuotesTheReplyAndEscalates(t *testing.T) {
	first := malformedCallNudge(1, `{"address":"A1","text":"x"}`, "")
	second := malformedCallNudge(2, `{"address":"A1","text":"x"}`, "")
	third := malformedCallNudge(3, `{"address":"A1","text":"x"}`, "excel_add_comment")
	if !strings.Contains(third, "LAST") || !strings.Contains(third, `"excel_add_comment", which is NOT a tool`) {
		t.Errorf("the third nudge is the last and names the non-tool: %q", third)
	}
	for _, s := range []string{first, second} {
		if !strings.Contains(s, `{"address":"A1","text":"x"}`) || !strings.Contains(s, `"name"`) || !strings.Contains(s, "Nothing ran") {
			t.Errorf("the nudge must quote the reply, show the working form and say nothing ran: %q", s)
		}
	}
	if first == second || strings.Contains(second, "LAST") || !strings.Contains(second, "again") {
		t.Error("the second nudge must differ from the first and not yet be the last")
	}
	long := malformedCallNudge(1, strings.Repeat("x", 2000), "")
	if len(long) > 1400 {
		t.Errorf("a huge reply is cut before it is quoted (%d bytes)", len(long))
	}
}

// consumeStream carries the adapter's flag through to the loop.
func TestConsumeStreamCarriesMalformedCall(t *testing.T) {
	a, _ := newApp(t, &fakeLLM{}, Config{Permission: "allow"})
	actor := event.Actor{Kind: event.ActorAgent, ID: "x"}
	ch := make(chan port.ProviderEvent, 3)
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: `{"address":"A1"}`}
	ch <- port.ProviderEvent{Type: port.ProviderFinish, FinishReason: "stop", MalformedCall: true}
	close(ch)
	res, err := a.consumeStream(context.Background(), session.SessionID("s_mal"), actor, ch, "m", func() {})
	if err != nil || !res.malformedCall {
		t.Errorf("malformedCall must be set from the finish event (err=%v res=%+v)", err, res.malformedCall)
	}
}
