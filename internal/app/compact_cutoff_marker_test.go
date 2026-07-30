package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// summarizerLLM answers any request with one text frame, optionally followed by the error the
// openai adapter now emits when a stream ends with no finish_reason and no [DONE] — the shape a
// connection dropped mid-summary produces.
type summarizerLLM struct {
	text string
	cut  bool
}

func (p summarizerLLM) StreamChat(ctx context.Context, req port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ch := make(chan port.ProviderEvent, 2)
	if p.text != "" {
		ch <- port.ProviderEvent{Type: port.ProviderText, Text: p.text}
	}
	if p.cut {
		ch <- port.ProviderEvent{Type: port.ProviderError,
			Err: errors.New("the model stream ended without finishing: no finish_reason and no [DONE] arrived")}
	}
	close(ch)
	return ch, nil
}

// The compaction summary BECOMES the session's memory of everything it replaces. When the model was
// cut off while writing it, magi knew — and said so only on the operator's progress channel. The
// reader that depends on the text is the agent, and for it this paragraph is now the whole of what
// came before; it cannot ask for what it does not know is missing.
//
// Every other truncation in magi marks itself inside the artifact it cut: the tool-result cap, the
// capture head/tail, the evidence block's dropped tail. This one did not.
func TestACutSummarySaysSoInsideItself(t *testing.T) {
	summarize := func(llm port.LLMProvider) string {
		t.Helper()
		a, sid, wd := newWorkflowApp(t, llm, nil, Config{Permission: "allow"})
		s := session.Session{ID: sid, Workdir: wd}
		msgs := []session.Message{{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: "hello"}}}}
		return a.summarizeViaLLM(context.Background(), AgentSpec{Name: "coder"}, s, msgs)
	}

	got := summarize(summarizerLLM{text: "The agent read shared_heap.c and started a build.", cut: true})
	if !strings.Contains(got, "started a build") {
		t.Errorf("what did arrive is still used — losing the region entirely is worse:\n%s", got)
	}
	for _, want := range []string{"INCOMPLETE", "cut off", "Treat gaps as unknown", "recall_context"} {
		if !strings.Contains(got, want) {
			t.Errorf("the summary must carry its own gap; missing %q:\n%s", want, got)
		}
	}

	// A summary that finished carries no such claim.
	if got := summarize(summarizerLLM{text: "A complete summary of the work so far."}); strings.Contains(got, "INCOMPLETE") {
		t.Errorf("nothing was cut, so nothing may say it was:\n%s", got)
	}

	// Cut before one character arrived: there is no summary to annotate, and the empty string is
	// what compactNow's own guard reads to refuse truncating the history at all.
	if got := summarize(summarizerLLM{cut: true}); got != "" {
		t.Errorf("an empty summary stays empty so compaction is skipped, got %q", got)
	}
}
