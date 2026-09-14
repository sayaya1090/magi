package idebridge

import (
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
)

func TestStreamedFactHasTimestampAfterReplacingDraft(t *testing.T) {
	for _, kind := range []string{"text", "reasoning"} {
		t.Run(kind, func(t *testing.T) {
			d := mk(1, "part.delta", map[string]any{"messageId": "m1", "kind": kind, "text": "hello"}, nil)
			f := mk(2, "part.appended", map[string]any{"messageId": "m1", "role": "assistant", "part": map[string]any{"kind": kind, "text": "hello"}}, nil)
			d.TS = time.Date(2026, 9, 14, 1, 0, 0, 0, time.UTC)
			f.TS = d.TS.Add(time.Second)
			streamed := Rows([]event.Event{d, f})
			fresh := Rows([]event.Event{f})
			if len(streamed) != 1 || len(fresh) != 1 {
				t.Fatalf("unexpected rows: %v / %v", streamed, fresh)
			}
			if streamed[0].At != stampOf(f.TS) || streamed[0].At != fresh[0].At {
				t.Fatalf("streamed timestamp %q differs from final fact %q", streamed[0].At, fresh[0].At)
			}
		})
	}
}
