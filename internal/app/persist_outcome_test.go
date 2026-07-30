package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

type recordingPersister struct {
	rules []string
	err   error
}

func (p *recordingPersister) PersistAllow(rule string) error {
	if p.err != nil {
		return p.err
	}
	p.rules = append(p.rules, rule)
	return nil
}

// notes returns the system notes the app appended to a session, in order.
func notesOf(t *testing.T, a *App, sid session.SessionID) []string {
	t.Helper()
	evs, err := a.store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range evs {
		if e.Type != event.TypePromptSubmitted || e.Actor.Kind != event.ActorSystem {
			continue
		}
		var d event.PromptSubmittedData
		if json.Unmarshal(e.Data, &d) != nil {
			continue
		}
		for _, p := range d.Parts {
			if strings.HasPrefix(p.Text, "note:") {
				out = append(out, p.Text)
			}
		}
	}
	return out
}

// The permission modal's third button is labelled `project`: the user is told the approval is
// being written where the project keeps it, so it outlives the run. Three things stop that and
// only one of them used to speak.
//
// In every silent case the SESSION grant still stands, so nothing looks wrong until the next run
// asks again — by which time the choice meant to prevent the prompt is long out of sight.
func TestTheProjectChoiceSaysSoWhenItWasNotWritten(t *testing.T) {
	bash := func(cmd string) *session.ToolCall {
		b, _ := json.Marshal(map[string]string{"command": cmd})
		return &session.ToolCall{CallID: "c1", Name: "bash", Args: b}
	}

	// Written: the rule lands and the transcript stays quiet — the modal already said what
	// `project` does, and repeating it on every success is noise.
	p := &recordingPersister{}
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "ask", PermissionPersister: p})
	a.notePersistOutcome(context.Background(), sid, bash("curl https://example.com/x"))
	if len(p.rules) != 1 || p.rules[0] != "bash(curl:*)" {
		t.Fatalf("the program name is what gets pinned, got %v", p.rules)
	}
	if n := notesOf(t, a, sid); len(n) != 0 {
		t.Errorf("a persisted rule needs no note: %v", n)
	}

	// No stable program name to pin to. Declining to write `bash(**)` is right; saying nothing
	// about it is not. `(cd /app && make)` is an ordinary shape, not a corner case.
	p2 := &recordingPersister{}
	b, sid2, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "ask", PermissionPersister: p2})
	b.notePersistOutcome(context.Background(), sid2, bash("(cd /app && make)"))
	if len(p2.rules) != 0 {
		t.Fatalf("a shell construct has no prefix to pin — nothing may be written, got %v", p2.rules)
	}
	n := notesOf(t, b, sid2)
	if len(n) != 1 {
		t.Fatalf("the user asked for a project rule and did not get one: %v", n)
	}
	for _, want := range []string{"THIS SESSION", "ask again", "bash(**)"} {
		if !strings.Contains(n[0], want) {
			t.Errorf("want %q in the note:\n%s", want, n[0])
		}
	}

	// Nowhere to write it.
	c, sid3, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "ask"})
	c.notePersistOutcome(context.Background(), sid3, bash("curl https://example.com/x"))
	n = notesOf(t, c, sid3)
	if len(n) != 1 || !strings.Contains(n[0], "no project config") {
		t.Fatalf("a run with nowhere to persist must say so: %v", n)
	}

	// The write was attempted and failed — the case that already spoke, still speaking, and now
	// naming the rule it failed on.
	p4 := &recordingPersister{err: errors.New("permission denied")}
	d, sid4, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "ask", PermissionPersister: p4})
	d.notePersistOutcome(context.Background(), sid4, bash("curl https://example.com/x"))
	n = notesOf(t, d, sid4)
	if len(n) != 1 || !strings.Contains(n[0], "bash(curl:*)") || !strings.Contains(n[0], "permission denied") {
		t.Fatalf("a failed write names the rule and the cause: %v", n)
	}

	// A non-bash tool pins the whole tool, which always has a rule to write.
	p5 := &recordingPersister{}
	e, sid5, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "ask", PermissionPersister: p5})
	e.notePersistOutcome(context.Background(), sid5, &session.ToolCall{CallID: "c2", Name: "webfetch", Args: json.RawMessage(`{"url":"https://x"}`)})
	if len(p5.rules) != 1 || p5.rules[0] != "webfetch(**)" {
		t.Fatalf("want webfetch(**), got %v", p5.rules)
	}
	if n := notesOf(t, e, sid5); len(n) != 0 {
		t.Errorf("nothing went wrong, so nothing is said: %v", n)
	}
}

// What persistRule writes, parseRule must read back — a rule that persists but never matches is a
// prompt the user thinks they silenced.
func TestAPersistedRuleMatchesTheCallItCameFrom(t *testing.T) {
	for _, c := range []struct{ tool, cmd, url string }{
		{tool: "bash", cmd: "curl https://example.com/a"},
		{tool: "bash", cmd: "git status --short"},
		{tool: "bash", cmd: "./configure --prefix=/usr"},
		{tool: "webfetch", url: "https://example.com/docs"},
	} {
		var args json.RawMessage
		if c.tool == "bash" {
			args, _ = json.Marshal(map[string]string{"command": c.cmd})
		} else {
			args, _ = json.Marshal(map[string]string{"url": c.url})
		}
		rule := persistRule(c.tool, args)
		if rule == "" {
			t.Errorf("%s %q: expected a rule", c.tool, c.cmd+c.url)
			continue
		}
		pol := newPolicy([]string{rule}, nil, nil)
		if !pol.AllowedByRule(c.tool, args) {
			t.Errorf("rule %q was written for %q but does not match it", rule, c.cmd+c.url)
		}
	}
}
