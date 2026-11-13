package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/session"
)

func callOf(name, args string) *session.ToolCall {
	return &session.ToolCall{CallID: "c_again_" + name, Name: name, Args: json.RawMessage(args)}
}

// A call that may have run before the restart is not repeated on the model's say-so: the same
// tool with the same arguments goes to the person even under "allow", and with no person to ask
// (headless) it is refused. A read-only look is never held, a different call is not this one, and
// once the person said repeating it is fine (or that it never ran) it runs like any other.
func TestRepeatingAnUncertainCallGoesToThePerson(t *testing.T) {
	ctx := context.Background()
	_, store, sid, wd := crashedLog(t, "c_write", "c_read")
	a := closeAfter(t, New(store, &stuckLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
	a.RecoverInterrupted(ctx, wd)
	actor := personActor

	if !a.gatePermission(ctx, sid, actor, callOf("write", `{}`), "m_t1") {
		t.Fatal("repeating the uncertain write must not run unasked")
	}
	if !a.gatePermission(ctx, sid, actor, callOf("write", ` { } `), "m_t2") {
		t.Fatal("the same arguments written differently are still the same call")
	}
	if a.gatePermission(ctx, sid, actor, callOf("write", `{"path":"other"}`), "m_t3") {
		t.Fatal("a different write is not a repeat")
	}
	if a.gatePermission(ctx, sid, actor, callOf("read", `{}`), "m_t4") {
		t.Fatal("a read-only look is never held for a check")
	}
	if err := a.ResolveCall(ctx, sid, "c_write", VerdictRan, "the file has it"); err != nil {
		t.Fatal(err)
	}
	if !a.gatePermission(ctx, sid, actor, callOf("write", `{}`), "m_t5") {
		t.Fatal("the person said it ran: repeating it would do it twice")
	}
}

func TestARepeatThePersonClearedRuns(t *testing.T) {
	for _, v := range []Verdict{VerdictRerunOK, VerdictNotRan} {
		t.Run(string(v), func(t *testing.T) {
			ctx := context.Background()
			_, store, sid, wd := crashedLog(t, "c_write")
			a := closeAfter(t, New(store, &stuckLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
			a.RecoverInterrupted(ctx, wd)
			if err := a.ResolveCall(ctx, sid, "c_write", v, ""); err != nil {
				t.Fatal(err)
			}
			if a.gatePermission(ctx, sid, personActor, callOf("write", `{}`), "m_t") {
				t.Fatalf("after %s the call runs like any other", v)
			}
		})
	}
}
