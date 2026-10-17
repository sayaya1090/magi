package app

import (
	"context"
	"fmt"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/model"
	"github.com/sayaya1090/magi/internal/core/session"
)

func TestContextOwnerUsesCurrentWindowWhileOfflineReaderKeepsRecordedWindow(t *testing.T) {
	for _, sizes := range [][2]int{{200000, 1000000}, {1000000, 200000}} {
		t.Run(fmt.Sprintf("%d-to-%d", sizes[0], sizes[1]), func(t *testing.T) {
			old, current := sizes[0], sizes[1]
			reg := model.NewRegistry()
			reg.Register(model.Info{ID: "window-model", ContextWindow: old, Tools: true})
			a, dir := newApp(t, &fakeLLM{}, Config{Models: reg})
			ctx := context.Background()
			sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: dir, Model: session.ModelRef{Model: "window-model"}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = a.store.Append(ctx, sid, ctxEvent(t, event.TypeTurnFinished, event.TurnFinishedData{Held: &event.Usage{In: 369000, Out: 402}, Prompt: &event.PromptShape{Window: old, System: 2000, Tools: 7000}}))
			if err != nil {
				t.Fatal(err)
			}
			// Metadata arrives after the recorded request (backend probe or an explicit override).
			reg.Register(model.Info{ID: "window-model", ContextWindow: current, Tools: true})
			got, err := a.ContextStateOf(ctx, sid)
			if err != nil {
				t.Fatal(err)
			}
			if got.Window != current || got.Used != 369402 {
				t.Fatalf("owner reading %+v; want window=%d, used=369402", got, current)
			}
			reader := New(a.store, nil, builtin.NewRegistry(), bus.New(), nil, Config{})
			historical, err := reader.ContextStateOf(ctx, sid)
			if err != nil {
				t.Fatal(err)
			}
			if historical.Window != old {
				t.Fatalf("offline reader lost recorded window: %+v", historical)
			}
		})
	}
}
