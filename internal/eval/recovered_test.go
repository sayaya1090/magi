package eval

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// cutThenFinishLLM streams a reply that is cut mid-stream (a recovered error), then a clean
// second step — the shape the runner used to score at the first error event.
type cutThenFinishLLM struct{ calls int }

func (f *cutThenFinishLLM) StreamChat(ctx context.Context, _ port.ChatRequest) (<-chan port.ProviderEvent, error) {
	f.calls++
	step := f.calls
	ch := make(chan port.ProviderEvent, 4)
	go func() {
		defer close(ch)
		_ = step
		ch <- port.ProviderEvent{Type: port.ProviderText, Text: "the prefix that stands"}
		ch <- port.ProviderEvent{Type: port.ProviderError,
			Err: fmt.Errorf("%w: cut mid-reply", port.ErrStreamCut)}
	}()
	return ch, nil
}

// The runner must not score the task at a RECOVERED error: the loop lands the turn with the
// prefix (turn.finished follows the recovered event), and the runner's job is to wait for that
// landing — noting "error: provider" at the recovered event was the same undone-one-layer-up
// mistake the headless CLI had.
func TestEvalKeepsWatchingPastARecoveredError(t *testing.T) {
	res, err := Run(&cutThenFinishLLM{}, "fake", nil, []Task{{
		Name:   "recovered-cut",
		Prompt: "start replying",
		Check: func(_ string, reply string, _ Result) (bool, string) {
			return strings.Contains(reply, "the prefix that stands"), "the prefix must be the scored reply"
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	r := res[0]
	if !r.Finished || !r.Success {
		t.Fatalf("the run landed with its prefix; the runner scored it early: %+v", r)
	}
	if strings.HasPrefix(r.Note, "error:") {
		t.Fatalf("a recovered error must not become the task's note: %q", r.Note)
	}
}
