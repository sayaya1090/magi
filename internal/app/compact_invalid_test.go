package app

import (
	"context"
	"encoding/json"
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
