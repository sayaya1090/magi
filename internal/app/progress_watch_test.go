package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// progressWatcher collects the transient tool-progress notes a session publishes. They go to the bus
// rather than the store, so a test that asserts on one has to be listening before the call.
type progressWatcher struct {
	mu   sync.Mutex
	seen []string
	stop func()
}

func watchProgress(t *testing.T, a *App, sid session.SessionID) *progressWatcher {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ch, unsub := a.bus.Subscribe(ctx, sid)
	w := &progressWatcher{stop: func() { unsub(); cancel() }}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for e := range ch {
			if e.Type != event.TypeToolProgress {
				continue
			}
			var d event.ToolProgressData
			if json.Unmarshal(e.Data, &d) != nil {
				continue
			}
			w.mu.Lock()
			w.seen = append(w.seen, d.Name+": "+d.Text)
			w.mu.Unlock()
		}
	}()
	t.Cleanup(func() { w.stop(); <-done })
	return w
}

// notes returns every note published under the given tool name, joined. It polls because the publish
// and the collecting goroutine are concurrent — and it waits for the count to STOP GROWING, not merely
// to become non-zero: a pass that emits several notes (a failure, its retry, the outcome) would
// otherwise return whichever ones happened to have landed, so an assertion over the whole sequence
// passed or failed by timing.
func (w *progressWatcher) notes(name string) string {
	var last []string
	stable := 0
	for i := 0; i < 200; i++ {
		w.mu.Lock()
		var hit []string
		for _, s := range w.seen {
			if strings.HasPrefix(s, name+": ") {
				hit = append(hit, s)
			}
		}
		w.mu.Unlock()
		if len(hit) > 0 && len(hit) == len(last) {
			if stable++; stable >= 4 { // ~20ms with no new note → the pass is done publishing
				return strings.Join(hit, "\n")
			}
		} else {
			stable = 0
		}
		last = hit
		time.Sleep(5 * time.Millisecond)
	}
	return strings.Join(last, "\n")
}
