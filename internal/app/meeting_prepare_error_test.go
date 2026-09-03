package app

import (
	"context"
	"errors"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/model"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// refuseLLM fails every call, the way a 429 or a dropped stream does.
type refuseLLM struct{}

func (refuseLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	return nil, errors.New("rate limited (status 429)")
}

// A prepare turn whose model failed must surface the failure, not report the participant ready.
//
// spawnChild always returns err=nil and stashes the model error in res.Err; MeetingPrepare used to
// check only err, so a 429'd participant came back ready with an empty brief and the room opened as
// if it had read its workspace. Meeting.Open's own doc names "a model that failed" as one that must
// carry Trouble — this is what makes that true.
func TestMeetingPrepareSurfacesAModelFailure(t *testing.T) {
	reg := model.NewRegistry()
	reg.Register(model.Info{ID: "m", ContextWindow: 8000, Tools: true})
	store, _ := jsonl.New(t.TempDir())
	a := New(store, refuseLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow", Models: reg})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir(), Model: session.ModelRef{Provider: "openai", Model: "m"}})

	_, ready, err := a.MeetingPrepare(context.Background(), sid, "beta", "should we bump the toolchain", nil)
	if err == nil {
		t.Fatalf("a prepare turn that failed at the model returned nil error and readiness %q — the room would open as if it prepared", ready)
	}
}
