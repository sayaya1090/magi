package daemon

import (
	"context"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
)

type refCatcher struct {
	fakeEngine
	got []command.FileRef
}

func (r *refCatcher) Submit(_ context.Context, c command.SubmitPrompt) error {
	r.got = c.Refs
	return nil
}
func (r *refCatcher) Steer(_ context.Context, c command.SubmitPrompt) error {
	r.got = c.Refs
	return nil
}

// The refs a client attaches must cross the wire into the command whole — a dropped attachment is
// the silent-wrong-answer shape: the prompt still reads fine and the agent answers without what
// the person pointed at.
func TestRefsCrossTheWireWhole(t *testing.T) {
	for _, method := range []string{"submit", "steer"} {
		e := &refCatcher{}
		refs := []command.FileRef{{Path: "a.go", Lines: "3-9"}, {Path: "b.md"}}
		if err := dispatchNow(context.Background(), e, Request{
			Method: method, Session: "s", Text: "see refs", Refs: refs}); err != nil {
			t.Fatal(err)
		}
		if len(e.got) != 2 || e.got[0].Lines != "3-9" || e.got[1].Path != "b.md" {
			t.Fatalf("%s: refs arrived as %+v", method, e.got)
		}
	}
}
