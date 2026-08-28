package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A tool this harness HAS, called by the name every other harness uses.
//
// Measured across the recorded runs: 134 of 10,580 calls named a tool that does not exist, in 21%
// of sessions — and 91 of those were `todo_write`/`todo`/`todo write` carrying the one argument
// `todos`. That is `todowrite`, spelled the way the model was trained to spell it. Refusing cost a
// round trip each and taught nothing: one run made the same call four times over four hours.
//
// So the call runs, and the result says which name is real. Both halves matter. Running it without
// saying so would be a side effect the model cannot see — the call worked, nothing would mark the
// name wrong, and the next one spells it wrong again.
func TestAnotherHarnessesToolNameRunsAndIsNamed(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	ctx := context.Background()
	s := a.sessionInfo(ctx, sid)
	actor := event.Actor{Kind: event.ActorAgent, ID: "coder"}
	todos := `{"todos":[{"content":"read the file","status":"in_progress"}]}`

	for _, called := range []string{"todo_write", "todo write", "todo"} {
		a.executeTool(ctx, s, AgentSpec{Name: "coder"}, 0, actor,
			&session.ToolCall{CallID: "c-" + called, Name: called, Args: json.RawMessage(todos)}, newRunGuard(nil), "")
		body := resultBody(t, a, sid, "c-"+called)
		if strings.HasPrefix(body, "unknown tool") {
			t.Errorf("%q was refused though it names a registered tool: %s", called, body)
			continue
		}
		if !strings.Contains(body, "`todowrite`") || !strings.Contains(body, "exact registered name") {
			t.Errorf("%q ran without being told the real name:\n%s", called, body)
		}
		if !strings.Contains(body, called) {
			t.Errorf("the note does not say WHICH name was wrong:\n%s", body)
		}
	}
	// It really ran — the todos are on the session, not just a friendly message.
	if td := a.Todos(sid); len(td) != 1 || td[0].Content != "read the file" {
		t.Errorf("the aliased call did not do the work, todos=%+v", td)
	}

	// A name that is not a registered tool under any spelling is still refused, with the roster.
	// `run` is the real one: 33 calls, arguments all over the place, nothing to resolve it to.
	a.executeTool(ctx, s, AgentSpec{Name: "coder"}, 0, actor,
		&session.ToolCall{CallID: "c-run", Name: "run",
			Args: json.RawMessage(`{"command":"ls"}`)}, newRunGuard(nil), "")
	body := resultBody(t, a, sid, "c-run")
	if !strings.HasPrefix(body, "unknown tool: run") {
		t.Errorf("an unresolvable name must still be refused, got:\n%s", body)
	}
	if !strings.Contains(body, "Available tools") {
		t.Errorf("the refusal must carry the roster:\n%s", body)
	}
}

// A tool the agent may not call is refused in the words the model used.
//
// The allowlist gate stops it either way — resolving the alias first cannot route around it,
// because the gate runs on the resolved name. What the resolution changes is the SENTENCE: without
// filtering the candidates by what this agent is allowed, the refusal comes back as "retrying
// todowrite will be refused the same way" — a spelling the model never typed, about a tool it was
// never told it had. It has nothing to do with that string, and no way to connect the advice to
// its own call.
func TestADisallowedToolIsRefusedInTheNameTheModelUsed(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	ctx := context.Background()
	s := a.sessionInfo(ctx, sid)
	actor := event.Actor{Kind: event.ActorAgent, ID: "reader"}
	// An agent whose allowlist has no todowrite.
	spec := AgentSpec{Name: "reader", Tools: []string{"read", "list"}}
	a.executeTool(ctx, s, spec, 0, actor,
		&session.ToolCall{CallID: "c1", Name: "todo_write",
			Args: json.RawMessage(`{"todos":[{"content":"x","status":"pending"}]}`)}, newRunGuard(nil), "")
	body := resultBody(t, a, sid, "c1")
	if strings.Contains(body, "exact registered name") {
		t.Errorf("the alias reached a tool the agent may not call:\n%s", body)
	}
	if td := a.Todos(sid); len(td) != 0 {
		t.Errorf("a disallowed tool ran through an alias, todos=%+v", td)
	}
	if !strings.Contains(body, "todo_write") {
		t.Errorf("the refusal does not name what the model actually called:\n%s", body)
	}
	if strings.Contains(body, "todowrite") {
		t.Errorf("the refusal advises about a spelling the model never used:\n%s", body)
	}
}
