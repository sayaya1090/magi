package llm

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

type oneLiner struct{ say string }

func (o oneLiner) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ch := make(chan port.ProviderEvent, 2)
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: o.say}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

// Advise is one careful reader answering in prose: the first member, the task framed above the
// question, and the words back untouched — no verdict machinery.
func TestAdviseIsOneReaderInProse(t *testing.T) {
	var got port.ChatRequest
	c := New(func(provider string) port.LLMProvider {
		return capture{&got, oneLiner{say: "Yes — the task names it."}}
	}, "m-default")
	out, err := c.Advise(context.Background(), port.AdviceRequest{
		Task: "build the thing", Question: "is the port required?"})
	if err != nil || out != "Yes — the task names it." {
		t.Fatalf("(%q, %v)", out, err)
	}
	sent := got.Messages[0].Parts[0].Text
	if !strings.Contains(sent, "── THE TASK ──") || !strings.Contains(sent, "is the port required?") {
		t.Fatalf("the task frames the question: %q", sent)
	}
	if got.Params["temperature"] != 0.0 {
		t.Fatal("one careful reader reads cold")
	}

	nobody := New(func(string) port.LLMProvider { return nil }, "m")
	if _, err := nobody.Advise(context.Background(), port.AdviceRequest{Question: "q"}); err == nil {
		t.Fatal("no backend resolved must be an error, not an empty answer")
	}
}

type capture struct {
	into  *port.ChatRequest
	inner oneLiner
}

func (c capture) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	*c.into = r
	return c.inner.StreamChat(ctx, r)
}

// noteSalvaged says what was kept and why on stderr — the run's record when a council reply
// arrives malformed.
func TestNoteSalvagedSpeaksOnStderr(t *testing.T) {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	noteSalvaged(`{"decision":"done","broken`, `{"decision":"done"}`)
	w.Close()
	os.Stderr = old
	b, _ := io.ReadAll(r)
	if !strings.Contains(string(b), "malformed") || !strings.Contains(string(b), "19 of 26") {
		t.Fatalf("the note names the salvage: %q", b)
	}
}

var _ = errors.New
var _ = session.Message{}
