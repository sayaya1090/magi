package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	luaplugin "github.com/sayaya1090/magi/internal/adapter/plugin/lua"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

const routineFile = `---
kind: routine
description: read a file and say so
tools: read
inputs: {"type":"object","properties":{"file":{"type":"string"}},"required":["file"]}
---
Reads the named file.

` + "```lua\nlocal text = call_tool(\"read\", {path=input.file})\nprint(\"read \" .. input.file .. \" \" .. tostring(#text > 0))\n```\n"

// routineRig is an app with the shipped dummy plug loaded over a plugin data dir that outlives it,
// so a second rig over the same dir is the daemon after a restart.
type routineRig struct {
	a    *App
	sid  session.SessionID
	s    session.Session
	host *luaplugin.Host
	logs *logBuf
}

func newRoutineRig(t *testing.T, dataDir, routine string) *routineRig {
	t.Helper()
	return newRoutineRigWith(t, dataDir, routine, Config{Permission: "ask", Interactive: true, AnswerWait: 5 * time.Second})
}

func newRoutineRigWith(t *testing.T, dataDir, routine string, cfg Config) *routineRig {
	t.Helper()
	return newRoutineRigAnalyzing(t, dataDir, routine, cfg, nil)
}

// newRoutineRigAnalyzing is newRoutineRigWith with a sidecar analyzer for magi.analyze (nil: none).
func newRoutineRigAnalyzing(t *testing.T, dataDir, routine string, cfg Config, an luaplugin.Analyzer) *routineRig {
	t.Helper()
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, cfg)
	ctx := context.Background()
	s := a.sessionInfo(ctx, sid)
	logs := &logBuf{}
	host := luaplugin.NewHostWithConfig(luaplugin.HostConfig{ToolSink: a.tools.(luaplugin.ToolSink), DataDir: dataDir,
		Runtime: luaplugin.RuntimeInfo{Workdir: s.Workdir}, Logf: logs.add, Analyzer: an})
	if _, err := host.Load(ctx, filepath.Join("..", "..", "plugins", "dummy-plug")); err != nil {
		t.Fatal(err)
	}
	r := &routineRig{a: a, sid: sid, s: s, host: host, logs: logs}
	r.write(t, routine)
	if err := os.WriteFile(filepath.Join(s.Workdir, "notes.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r *routineRig) write(t *testing.T, routine string) {
	t.Helper()
	dir := filepath.Join(r.s.Workdir, ".magi", "skills")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "readit.md"), []byte(routine), 0o600); err != nil {
		t.Fatal(err)
	}
}

// run calls run_routine and answers each approval prompt with decide; it returns the result, the
// prompts shown and whether the result was an error.
func (r *routineRig) run(t *testing.T, id string, args map[string]any, decide string) (string, []Ask, bool) {
	t.Helper()
	return r.call(t, id, "run_routine", map[string]any{"name": "readit", "args": args}, decide)
}

// call runs a tool and answers each prompt it raises with decide.
func (r *routineRig) call(t *testing.T, id, tool string, in map[string]any, decide string) (string, []Ask, bool) {
	t.Helper()
	raw, _ := json.Marshal(in)
	outer := &session.ToolCall{CallID: id, Name: tool, Args: raw}
	ctx := context.Background()
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.a.executeTool(ctx, r.s, AgentSpec{Name: "coder"}, 0, event.Actor{Kind: event.ActorAgent, ID: "coder"}, outer, newRunGuard(nil), "")
	}()
	var asks []Ask
	answered := map[string]bool{}
	for {
		select {
		case <-done:
			evs, _ := r.a.store.Read(ctx, r.sid, 0)
			for i := len(evs) - 1; i >= 0; i-- {
				var d event.PartAppendedData
				if evs[i].Type == event.TypePartAppended && json.Unmarshal(evs[i].Data, &d) == nil && d.Part.ToolResult != nil && d.Part.ToolResult.CallID == id {
					var said string
					if json.Unmarshal(d.Part.ToolResult.Content, &said) != nil {
						said = string(d.Part.ToolResult.Content)
					}
					return said, asks, d.Part.ToolResult.IsError
				}
			}
			t.Fatalf("%s left no result", id)
		case <-time.After(10 * time.Millisecond):
			if ask, ok := r.a.Waiting(r.sid); ok && !answered[ask.ID] {
				answered[ask.ID] = true
				asks = append(asks, ask)
				if err := r.a.RespondPermission(ctx, command.RespondPermission{SessionID: r.sid, CallID: ask.ID, Decision: decide}); err != nil {
					t.Fatal(err)
				}
			}
		case <-time.After(20 * time.Second):
			t.Fatalf("%s never finished", id)
		}
	}
}

// A routine is run by name. Its first run shows the person what runs and which tools it may call;
// once approved, it runs again without asking — also after a restart, from what was stored — and
// its calls are recorded in a child session.
func TestARoutineIsApprovedOnceAndRunsByNameAfterARestart(t *testing.T) {
	data := t.TempDir()
	r := newRoutineRig(t, data, routineFile)
	out, asks, failed := r.run(t, "r1", map[string]any{"file": "notes.txt"}, "allow")
	if failed || !strings.Contains(out, "read notes.txt true") {
		t.Fatalf("first run: %q (failed=%v)", out, failed)
	}
	if len(asks) != 1 || asks[0].ID != "r1/approve" || !strings.Contains(asks[0].Reason, "read") ||
		!strings.Contains(string(asks[0].Args), `call_tool(\"read\"`) || !strings.Contains(string(asks[0].Args), `"tools":["read"]`) ||
		!strings.Contains(string(asks[0].Args), `"required":["file"]`) {
		t.Fatalf("the first run must show the script and its tools once: %+v", asks)
	}
	if kids, err := r.a.store.ChildSessions(context.Background(), r.s.Workdir, string(r.sid)); err != nil || len(kids) != 1 {
		t.Fatalf("the routine's calls belong in one child session, got %v (%v)", kids, err)
	}
	if out, asks, _ = r.run(t, "r2", map[string]any{"file": "notes.txt"}, "deny"); len(asks) != 0 || !strings.Contains(out, "read notes.txt true") {
		t.Fatalf("an approved routine runs again without asking: %q %+v", out, asks)
	}
	restarted := newRoutineRig(t, data, routineFile)
	if out, asks, _ = restarted.run(t, "r3", map[string]any{"file": "notes.txt"}, "deny"); len(asks) != 0 || !strings.Contains(out, "read notes.txt true") {
		t.Fatalf("after a restart the approval must still hold: %q %+v", out, asks)
	}
}

// What was approved is the script, the input schema and the tool list together: changing any of
// them asks again, an "always" answer does not stand in for a later change, and a refusal runs nothing.
func TestARoutineAsksAgainWhenItsScriptInputsOrToolsChange(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	if _, asks, _ := r.run(t, "a1", map[string]any{"file": "notes.txt"}, "always"); len(asks) != 1 {
		t.Fatalf("first run asks once, got %d", len(asks))
	}
	changes := map[string]string{
		"script": strings.Replace(routineFile, "tostring(#text > 0)", "tostring(#text > 1)", 1),
		"inputs": strings.Replace(routineFile, `"required":["file"]`, `"required":[]`, 1),
		"tools":  strings.Replace(routineFile, "tools: read", "tools: read, glob", 1),
	}
	for _, what := range []string{"script", "inputs", "tools"} {
		r.write(t, changes[what])
		out, asks, failed := r.run(t, "c-"+what, map[string]any{"file": "notes.txt"}, "deny")
		if len(asks) != 1 || !strings.Contains(asks[0].Reason, "changed since it was approved") {
			t.Fatalf("changing the %s must ask again: %+v", what, asks)
		}
		if !failed || !strings.Contains(out, "not approved; nothing ran") {
			t.Fatalf("a refused %s change must run nothing: %q", what, out)
		}
	}
	if kids, _ := r.a.store.ChildSessions(context.Background(), r.s.Workdir, string(r.sid)); len(kids) != 1 {
		t.Fatalf("refused runs opened children: %d", len(kids))
	}
}

// Approving a routine is not a grant for its calls: a tool outside the approved list is refused at
// the call, a routine naming a writing tool is refused before it asks, and bad inputs never reach it.
func TestARoutineReachesOnlyTheToolsItWasApprovedFor(t *testing.T) {
	sneaky := strings.Replace(routineFile, `print("read " .. input.file .. " " .. tostring(#text > 0))`,
		`print("read " .. input.file .. " " .. tostring(#text > 0)) print(call_tool("glob", {pattern="*"}))`, 1)
	if sneaky == routineFile {
		t.Fatal("fixture did not change")
	}
	r := newRoutineRig(t, t.TempDir(), sneaky)
	// call_tool hands a refused call back as nil and the reason; the call itself never runs, so the
	// child records the one read and not the glob.
	out, _, _ := r.run(t, "s1", map[string]any{"file": "notes.txt"}, "allow")
	if !strings.Contains(out, "glob is not declared") || !strings.Contains(out, ", 1 tool call(s)]") {
		t.Fatalf("a call outside the approved tools must be refused and not run: %q", out)
	}
	r.write(t, strings.Replace(routineFile, "tools: read", "tools: read, coding_task", 1))
	if out, asks, failed := r.run(t, "s2", map[string]any{"file": "notes.txt"}, "allow"); !failed || len(asks) != 0 || !strings.Contains(out, "coding_task cannot be called from a routine") {
		t.Fatalf("a routine naming a tool it cannot call must be refused before it asks: %q %+v", out, asks)
	}
	r.write(t, routineFile)
	if out, asks, failed := r.run(t, "s3", map[string]any{"other": 1}, "allow"); !failed || len(asks) != 0 || !strings.Contains(out, "missing input file") {
		t.Fatalf("inputs that do not fit the schema must be refused before it asks: %q %+v", out, asks)
	}
}

// fakeTool stands in for a tool that needs a model (explore) or a memory store (recall_memory): it
// records what it was asked and answers from it, so a routine's flow can be checked without either.
type fakeTool struct {
	name   string
	answer func(args map[string]any) string
	got    []map[string]any
}

func (f *fakeTool) Name() string            { return f.name }
func (f *fakeTool) Description() string     { return "fake " + f.name }
func (f *fakeTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (f *fakeTool) Execute(_ context.Context, raw json.RawMessage, _ port.ToolEnv) (session.ToolResult, error) {
	var args map[string]any
	_ = json.Unmarshal(raw, &args)
	f.got = append(f.got, args)
	b, _ := json.Marshal(f.answer(args))
	return session.ToolResult{Content: b}, nil
}

// The shipped example: the recovery policy is recalled once and handed to every explore, and each
// question's answer comes back under it.
func TestTheRecoveryPolicyRoutineHandsThePolicyToEachExplore(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "plugins", "dummy-plug", "routines", "recovery-policy-evidence.md"))
	if err != nil {
		t.Fatal(err)
	}
	r := newRoutineRig(t, t.TempDir(), routineFile)
	if err := os.WriteFile(filepath.Join(r.s.Workdir, ".magi", "skills", "recovery-policy-evidence.md"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	recall := &fakeTool{name: "recall_memory", answer: func(map[string]any) string { return "POLICY: an interrupted call is not re-run on the model's say-so" }}
	explore := &fakeTool{name: "explore", answer: func(a map[string]any) string {
		q, _ := a["question"].(string)
		return "evidence for " + q[strings.LastIndex(q, "Question: ")+10:strings.Index(q, "\nAnswer")]
	}}
	r.a.tools.Register(recall)
	r.a.tools.Register(explore)
	out, asks, failed := r.call(t, "rp", "run_routine", map[string]any{"name": "recovery-policy-evidence",
		"args": map[string]any{"questions": []any{"Q1 restart", "Q2 permission"}}}, "allow")
	if failed || len(asks) != 1 || !strings.Contains(string(asks[0].Args), `"tools":["explore","recall_memory"]`) {
		t.Fatalf("the example must ask once for recall_memory and explore and then run: %q %+v", out, asks)
	}
	if len(recall.got) != 1 || len(explore.got) != 2 {
		t.Fatalf("want one recall and one explore per question, got %d and %d", len(recall.got), len(explore.got))
	}
	for _, e := range explore.got {
		if q, _ := e["question"].(string); !strings.Contains(q, "POLICY: an interrupted call") {
			t.Fatalf("an explore did not get the recalled policy: %q", q)
		}
	}
	if !strings.Contains(out, "## Q1 restart\nevidence for Q1 restart") || !strings.Contains(out, "## Q2 permission\nevidence for Q2 permission") {
		t.Fatalf("each answer must come back under its question: %q", out)
	}
}

// A read-only script the model writes again and again becomes a routine candidate — offered, not
// registered. Only the person registers it (a command), and its first run still asks for approval.
func TestARepeatedScriptIsOfferedAsARoutineAndRegisteredOnlyByThePerson(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	script := `local files = json_decode(call_tool("glob", {pattern="*.txt"})) print(#files) print(call_tool("read", {path="notes.txt"}) ~= nil)`
	var out string
	for i := 1; i <= 3; i++ {
		out, _, _ = r.call(t, fmt.Sprintf("d%d", i), "dummy_plug", map[string]any{"goal": "count the notes", "script": script}, "allow")
		if offered := strings.Contains(out, "[routine candidate "); offered != (i == 3) {
			t.Fatalf("run %d: candidate offered=%v: %q", i, offered, out)
		}
	}
	m := regexp.MustCompile(`routine candidate (c[0-9a-f]{8})`).FindStringSubmatch(out)
	if m == nil || !strings.Contains(out, "glob>read") {
		t.Fatalf("no candidate id: %q", out)
	}
	path := filepath.Join(r.s.Workdir, ".magi", "skills", "countnotes.md")
	if _, err := os.Stat(path); err == nil {
		t.Fatal("a candidate was registered before the person registered it")
	}
	var register interface {
		Name() string
		Execute([]string) error
	}
	for _, c := range r.host.PluginCommands() {
		if c.Name() == "routine-register" {
			register = c
		}
	}
	if register == nil {
		t.Fatal("no /routine-register")
	}
	if err := register.Execute([]string{m[1], "countnotes"}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(body), "kind: routine") || !strings.Contains(string(body), "tools: glob, read") || !strings.Contains(string(body), script) {
		t.Fatalf("registered routine: %q (%v)", body, err)
	}
	out, asks, failed := r.call(t, "rc", "run_routine", map[string]any{"name": "countnotes"}, "allow")
	if failed || len(asks) != 1 || !strings.Contains(out, "true") {
		t.Fatalf("the registered routine must ask once and run: %q %+v", out, asks)
	}
}

// Approving a routine lets it run; it does not let its calls past the gate. Approved with "always",
// the routine runs again without being asked about — and its read of a secret path is still refused
// by the policy, exactly as the same read would be outside a routine.
func TestAnApprovedRoutinesCallsStillPassThePermissionGate(t *testing.T) {
	guarded := strings.Replace(routineFile, `local text = call_tool("read", {path=input.file})
print("read " .. input.file .. " " .. tostring(#text > 0))`,
		`local text, why = call_tool("read", {path=input.file})
print(text and ("read " .. input.file) or ("refused: " .. tostring(why)))`, 1)
	if guarded == routineFile {
		t.Fatal("fixture did not change")
	}
	r := newRoutineRig(t, t.TempDir(), guarded)
	if err := os.WriteFile(filepath.Join(r.s.Workdir, ".env"), []byte("TOKEN=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, asks, _ := r.run(t, "g1", map[string]any{"file": "notes.txt"}, "always"); len(asks) != 1 || !strings.Contains(out, "read notes.txt") {
		t.Fatalf("first run: %q %+v", out, asks)
	}
	out, asks, _ := r.run(t, "g2", map[string]any{"file": ".env"}, "allow")
	if len(asks) != 0 {
		t.Fatalf("the approved routine was asked about again: %+v", asks)
	}
	if !strings.Contains(out, "refused: ") || !strings.Contains(out, "blocked by policy") || strings.Contains(out, "TOKEN") {
		t.Fatalf("the routine's read of a secret must still be refused by the policy: %q", out)
	}
}

// A routine's approval is a person's, never a policy's. With nobody to ask (a headless run), or
// nobody answering in time, an "allow" permission mode does not approve it: nothing runs and nothing
// is recorded as approved, so the next run with a person asks.
func TestARoutineIsNotApprovedByPolicyWithoutAPerson(t *testing.T) {
	data := t.TempDir()
	headless := newRoutineRigWith(t, data, routineFile, Config{Permission: "allow"})
	out, _, failed := headless.run(t, "h1", map[string]any{"file": "notes.txt"}, "allow")
	if !failed || strings.Contains(out, "read notes.txt") || !strings.Contains(out, "not approved") {
		t.Fatalf("a headless allow run approved the routine: %q", out)
	}
	slow := newRoutineRigWith(t, data, routineFile, Config{Permission: "allow", Interactive: true, AnswerWait: 50 * time.Millisecond})
	slow.write(t, routineFile)
	unanswered := &routineRig{a: slow.a, sid: slow.sid, s: slow.s, host: slow.host}
	raw, _ := json.Marshal(map[string]any{"name": "readit", "args": map[string]any{"file": "notes.txt"}})
	outer := &session.ToolCall{CallID: "t1", Name: "run_routine", Args: raw}
	unanswered.a.executeTool(context.Background(), unanswered.s, AgentSpec{Name: "coder"}, 0, event.Actor{Kind: event.ActorAgent, ID: "coder"}, outer, newRunGuard(nil), "")
	if kids, _ := unanswered.a.store.ChildSessions(context.Background(), unanswered.s.Workdir, string(unanswered.sid)); len(kids) != 0 {
		t.Fatalf("an unanswered approval ran the routine: %d children", len(kids))
	}
	asked := newRoutineRig(t, data, routineFile)
	if _, asks, _ := asked.run(t, "p1", map[string]any{"file": "notes.txt"}, "deny"); len(asks) != 1 {
		t.Fatalf("nothing should have been recorded as approved; the next run must ask: %+v", asks)
	}
}

// Answering a routine's approval with "always" or "persist" approves that routine version only: it
// grants no standing permission for run_routine and writes no project allow rule.
func TestARoutineApprovalGrantsNoToolPermission(t *testing.T) {
	for _, answer := range []string{"always", "persist"} {
		r := newRoutineRig(t, t.TempDir(), routineFile)
		if out, asks, failed := r.run(t, "a-"+answer, map[string]any{"file": "notes.txt"}, answer); failed || len(asks) != 1 || !strings.Contains(out, "read notes.txt") {
			t.Fatalf("%s: the routine must run once approved: %q %+v", answer, out, asks)
		}
		r.a.mu.Lock()
		granted := false
		if st, ok := r.a.stateIf(r.sid); ok {
			granted = st.grants["run_routine"]
		}
		r.a.mu.Unlock()
		if granted {
			t.Fatalf("%s: a routine approval granted the run_routine tool for the session", answer)
		}
		if b, err := os.ReadFile(filepath.Join(r.s.Workdir, ".magi", "config.toml")); err == nil && strings.Contains(string(b), "run_routine") {
			t.Fatalf("%s: a routine approval wrote a project allow rule: %s", answer, b)
		}
	}
}
