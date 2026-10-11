package app

import (
	"context"
	"encoding/json"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
	"strings"
	"testing"
	"time"
)

type preparationCouncil struct {
	fakeCouncil
	requests chan port.DeliberationRequest
}

func (c *preparationCouncil) Prepare(ctx context.Context, r port.DeliberationRequest) error {
	select {
	case c.requests <- r:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func TestPersistedEvidenceAndJudgmentsWakePreparation(t *testing.T) {
	c := &preparationCouncil{requests: make(chan port.DeliberationRequest, 8)}
	a, wd := newApp(t, &fakeLLM{}, Config{Council: c})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	w := a.startCouncilPreparation(ctx, a.sessionInfo(ctx, sid))
	defer w.close()
	data, _ := json.Marshal(event.PartAppendedData{Role: session.RoleAssistant, Part: session.Part{Kind: session.PartText, Text: "evidence A"}})
	if err := a.appendFact(ctx, sid, event.TypePartAppended, event.Actor{Kind: event.ActorAgent}, data); err != nil {
		t.Fatal(err)
	}
	wait := func() port.DeliberationRequest {
		t.Helper()
		select {
		case r := <-c.requests:
			return r
		case <-time.After(2 * time.Second):
			t.Fatal("persisted event did not wake preparation")
			return port.DeliberationRequest{}
		}
	}
	first := wait()
	if err := a.appendFact(ctx, sid, event.TypeCouncilDecided, event.Actor{Kind: event.ActorSystem}, json.RawMessage(`{"decision":"continue","feedback":"fix B"}`)); err != nil {
		t.Fatal(err)
	}
	second := wait()
	if !strings.HasPrefix(second.EvidencePrefix, first.EvidencePrefix) || !strings.Contains(second.EvidencePrefix, "fix B") {
		t.Fatal("judgment did not extend prepared prefix")
	}
	cancel()
	select {
	case <-w.done:
	case <-time.After(time.Second):
		t.Fatal("turn cancellation did not stop worker")
	}
}
