package app

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
)

const overflowSummary = "Prompt is too long · the request is ~1193065 tokens (limit 1000000) but this conversation is only ~554231 tokens — the rest is system prompt, tool definitions, and attachment content. A single-exchange conversation cannot be compacted; reduce attached files/tools or start with less context."

func TestProviderErrorTextCannotReplaceConversation(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, summarizerLLM{text: overflowSummary}, nil, Config{Permission: "allow"})
	if _, err := a.store.Append(context.Background(), sid, ctxEvent(t, event.TypePromptSubmitted, event.PromptSubmittedData{MessageID: "original", Parts: []session.Part{{Kind: session.PartText, Text: "preserve the original requirements"}}})); err != nil {
		t.Fatal(err)
	}
	before, err := a.store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Compact(context.Background(), command.Compact{SessionID: sid}); err == nil {
		t.Fatal("error text accepted as a summary")
	}
	after, err := a.store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	if countType(after, event.TypeCompaction) != countType(before, event.TypeCompaction) {
		t.Fatal("failed summary committed")
	}
}

func TestLegacyErrorCompactionRestoresOriginalModelHistory(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, summarizerLLM{text: "ok"}, nil, Config{Permission: "allow"})
	if _, err := a.store.Append(context.Background(), sid, ctxEvent(t, event.TypePromptSubmitted, event.PromptSubmittedData{MessageID: "original", Parts: []session.Part{{Kind: session.PartText, Text: "preserve the original requirements"}}})); err != nil {
		t.Fatal(err)
	}
	evs, err := a.store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	boundary := evs[len(evs)-1].Seq
	data, _ := json.Marshal(event.CompactionData{Summary: overflowSummary, ReplacesUpToSeq: boundary, TokensBefore: 616869, TokensAfter: 73})
	evs = append(evs, event.Event{Seq: boundary + 1, Type: event.TypeCompaction, Data: data})
	var text strings.Builder
	for _, m := range reconstruct(evs) {
		for _, p := range m.Parts {
			text.WriteString(p.Text)
		}
	}
	if !strings.Contains(text.String(), "preserve the original requirements") {
		t.Fatal("original history hidden by error summary")
	}
	if strings.Contains(text.String(), briefHeader) {
		t.Fatal("error became a memory brief")
	}
	if lastCompactionBoundary(evs) != 0 || lastCompactionSeq(evs) != 0 || priorSummary(evs, boundary+1) != "" {
		t.Fatal("failed fold still constrains next compaction")
	}
}

type chunkRecordingLLM struct {
	calls  []string
	failAt int
}

func (p *chunkRecordingLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	text := r.Messages[0].Parts[0].Text
	p.calls = append(p.calls, text)
	answer := "brief"
	if p.failAt == len(p.calls) {
		answer = overflowSummary
	}
	ch := make(chan port.ProviderEvent, 1)
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: answer}
	close(ch)
	return ch, nil
}
func TestLargeSummaryChunksCoverUTF8SourceAndFailAtomically(t *testing.T) {
	source := strings.Repeat("한글 문맥 αβ ", summaryMaterialBytes/8)
	for _, failAt := range []int{0, 2} {
		llm := &chunkRecordingLLM{failAt: failAt}
		a, sid, wd := newWorkflowApp(t, llm, nil, Config{Permission: "allow"})
		out := a.summarizeMaterial(context.Background(), a.agentFor(session.Session{ID: sid}), session.Session{ID: sid, Workdir: wd}, "summarize", source)
		if failAt > 0 {
			if out != "" || len(llm.calls) != failAt {
				t.Fatalf("partial chunk accepted: %q %d", out, len(llm.calls))
			}
			continue
		}
		var chunks strings.Builder
		for _, call := range llm.calls[:len(llm.calls)-1] {
			if len(call) > summaryMaterialBytes || !utf8.ValidString(call) {
				t.Fatal("invalid/oversized chunk")
			}
			chunks.WriteString(call)
		}
		if out == "" || chunks.String() != source {
			t.Fatal("chunking lost or reordered source")
		}
	}
}

type hardSummaryErrorLLM struct{}

func (hardSummaryErrorLLM) StreamChat(context.Context, port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ch := make(chan port.ProviderEvent, 2)
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: "a misleading error explanation"}
	ch <- port.ProviderEvent{Type: port.ProviderError, Err: fmt.Errorf("backend request rejected")}
	close(ch)
	return ch, nil
}
func TestHardSummaryErrorNeverBecomesIncompleteMemory(t *testing.T) {
	a, sid, wd := newWorkflowApp(t, hardSummaryErrorLLM{}, nil, Config{Permission: "allow"})
	if got := a.askForBrief(context.Background(), a.agentFor(session.Session{ID: sid}), session.Session{ID: sid, Workdir: wd}, "summarize", summaryMessage("history")); got != "" {
		t.Fatalf("hard failure accepted: %q", got)
	}
}
func TestCompactionAfterCountIncludesHeadersAndRecallHints(t *testing.T) {
	raw, _ := json.Marshal(event.PromptSubmittedData{MessageID: "m", Parts: []session.Part{{Kind: session.PartText, Text: strings.Repeat("original", 100)}}})
	evs := []event.Event{{Seq: 1, Type: event.TypePromptSubmitted, Data: raw}}
	d := event.CompactionData{Summary: "brief", ReplacesUpToSeq: 1, Shards: []event.ContextShard{{Topic: "source/file.go", Brief: "recover the original", MessageIDs: []string{"m"}}}}
	got := compactedHistoryTokens(evs, d)
	if got <= estimateTokens(d.Summary, nil) {
		t.Fatalf("headers/hints omitted: %d", got)
	}
	encoded, _ := json.Marshal(d)
	evs = append(evs, event.Event{Seq: 2, Type: event.TypeCompaction, Data: encoded})
	if want := estimateTokens("", reconstruct(evs)); got != want {
		t.Fatalf("reported %d != next reconstructed history %d", got, want)
	}
}

func TestManualCompactionElidesDigestedOutputBeforeSummarizing(t *testing.T) {
	llm := &chunkRecordingLLM{}
	a, sid, _ := newWorkflowApp(t, llm, nil, Config{Permission: "allow"})
	marker := strings.Repeat("OLD-BULKY-OUTPUT", 400)
	fresh := strings.Repeat("LATEST-OUTPUT", 400)
	for _, e := range []event.Event{elideCall(t, "old", "read"), elidePart(t, "old", marker, session.RoleTool), elideText(t, "digest", "The file uses the stable API."), elideCall(t, "fresh", "read"), elidePart(t, "fresh", fresh, session.RoleTool)} {
		if _, err := a.store.Append(context.Background(), sid, e); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Compact(context.Background(), command.Compact{SessionID: sid}); err != nil {
		t.Fatal(err)
	}
	if len(llm.calls) != 1 || strings.Contains(llm.calls[0], marker) || !strings.Contains(llm.calls[0], fresh) || !strings.Contains(llm.calls[0], "stable API") {
		t.Fatal("manual summarizer did not receive safely reduced material")
	}
	evs, err := a.store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	elidedAt, foldAt := int64(0), int64(0)
	for _, e := range evs {
		if e.Type == event.TypeResultElided {
			elidedAt = e.Seq
		}
		if e.Type == event.TypeCompaction {
			foldAt = e.Seq
		}
	}
	if elidedAt == 0 || foldAt <= elidedAt {
		t.Fatal("summary committed before tool-output elision")
	}
	foundRaw := false
	for _, m := range reconstructWhole(evs) {
		for _, p := range m.Parts {
			if p.ToolResult != nil && strings.Contains(string(p.ToolResult.Content), marker) {
				foundRaw = true
			}
		}
	}
	if !foundRaw {
		t.Fatal("elision deleted original transcript output")
	}
}
