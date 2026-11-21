package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// logBuf keeps what the plugins logged: a command's output goes to the log the person reads.
type logBuf struct {
	mu    sync.Mutex
	lines []string
}

func (b *logBuf) add(s string) { b.mu.Lock(); b.lines = append(b.lines, s); b.mu.Unlock() }
func (b *logBuf) text() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Join(b.lines, "\n")
}

func (r *routineRig) command(t *testing.T, name string, args ...string) error {
	t.Helper()
	for _, c := range r.host.PluginCommands() {
		if c.Name() == name {
			return c.Execute(args)
		}
	}
	t.Fatalf("no /%s", name)
	return nil
}

// fire hands the plugins an observation event of this conversation and waits until they handled it.
func (r *routineRig) fire(event string, kv ...string) {
	m := map[string]string{"session": string(r.sid)}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	r.host.FireEventWith(event, m)
	r.host.DrainEvents(5 * time.Second)
}

func (r *routineRig) file(t *testing.T, rel, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(r.s.Workdir, rel), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (r *routineRig) skill(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(r.s.Workdir, ".magi", "skills", name+".md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A stretch of the conversation's tool calls the person picks becomes a routine draft: each call a
// call_tool line, the values that vary its inputs, checked dry against the trace. A value that first
// shows up mid-stretch and is not in the request probably came from an earlier result: it is marked
// TODO and the routine stays a draft, which run_routine refuses until the person resolves it.
func TestAStretchOfTheTraceBecomesARoutineDraftCheckedDry(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	r.file(t, "abc1234.txt", "a\n")
	r.file(t, "deadbeef9.txt", "d\n")
	r.fire("user_message", "text", "read abc1234.txt for me")
	r.fire("tool_finished", "tool", "read", "args", `{"path":"notes.txt"}`, "ok", "true")
	r.fire("tool_finished", "tool", "read", "args", `{"path":"abc1234.txt"}`, "ok", "true")
	r.fire("tool_finished", "tool", "read", "args", `{"path":"deadbeef9.txt"}`, "ok", "true")
	if err := r.command(t, "routine-trace"); err != nil || !strings.Contains(r.logs.text(), "#3 turn 1 read") {
		t.Fatalf("/routine-trace: %v %q", err, r.logs.text())
	}
	if err := r.command(t, "routine-draft", "finder", "1-3"); err != nil {
		t.Fatal(err)
	}
	body := r.skill(t, "finder")
	for _, want := range []string{"kind: routine", "status: draft", "tools: read", `example: {"p1":"abc1234","p2":"deadbeef9"}`,
		"dry_check: passed 3/3", "-- TODO: input.p2 (deadbeef9)", "{{p1}}.txt", "source: trace "} {
		if !strings.Contains(body, want) {
			t.Fatalf("draft lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "TODO: input.p1") {
		t.Fatalf("a value from the request is an input, not a TODO:\n%s", body)
	}
	args := map[string]any{"p1": "abc1234", "p2": "deadbeef9"}
	out, asks, failed := r.call(t, "f1", "run_routine", map[string]any{"name": "finder", "args": args}, "allow")
	if !failed || len(asks) != 0 || !strings.Contains(out, "is a draft") {
		t.Fatalf("a draft must not run or ask: %q %+v", out, asks)
	}
	r.file(t, filepath.Join(".magi", "skills", "finder.md"), strings.Replace(body, "status: draft\n", "", 1))
	out, asks, failed = r.call(t, "f2", "run_routine", map[string]any{"name": "finder", "args": args}, "allow")
	if failed || len(asks) != 1 || !strings.Contains(out, "done: 3 steps") {
		t.Fatalf("once reviewed, the routine asks once and runs: %q %+v", out, asks)
	}
}

// A stretch that runs again in another turn, with only its varying values changed, is offered as a
// candidate on the same path: the person registers it, it is drafted and checked dry like a picked
// stretch, and its first run asks. A stretch seen once is not offered.
func TestARepeatedStretchOfTheTraceIsOfferedThroughTheSameDraftPath(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	r.file(t, "a1b2c3d4.txt", "a\n")
	r.file(t, "f9e8d7c6.txt", "f\n")
	turn := func(file string) {
		r.fire("user_message", "text", "open "+file)
		r.fire("tool_finished", "tool", "grep", "args", `{"path":"notes.txt","pattern":"hello"}`, "ok", "true")
		r.fire("tool_finished", "tool", "read", "args", `{"path":"`+file+`"}`, "ok", "true")
		r.fire("turn_finished")
	}
	turn("a1b2c3d4.txt")
	if strings.Contains(r.logs.text(), "[routine] candidate") {
		t.Fatalf("a stretch seen once was offered: %q", r.logs.text())
	}
	turn("f9e8d7c6.txt")
	m := regexp.MustCompile(`candidate (t[0-9a-f]{8})`).FindStringSubmatch(r.logs.text())
	if m == nil {
		t.Fatalf("a stretch seen in two turns was not offered: %q", r.logs.text())
	}
	if err := r.command(t, "routine-register", m[1], "opener"); err != nil {
		t.Fatal(err)
	}
	body := r.skill(t, "opener")
	for _, want := range []string{"kind: routine", "tools: grep, read", "dry_check: passed 2/2", "{{p1}}.txt"} {
		if !strings.Contains(body, want) {
			t.Fatalf("registered candidate lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "status: draft") {
		t.Fatalf("a stretch whose values all come from the request is not a draft:\n%s", body)
	}
	out, asks, failed := r.call(t, "o1", "run_routine", map[string]any{"name": "opener", "args": map[string]any{"p1": "a1b2c3d4"}}, "allow")
	if failed || len(asks) != 1 || !strings.Contains(out, "done: 2 steps") {
		t.Fatalf("the registered routine asks once and runs: %q %+v", out, asks)
	}
}

// A finished dummy_plug run is the other thing a person can save: /routine-trace lists it, and
// /routine-draft <name> run:<id> saves its script as it ran.
func TestADummyPlugRunIsSavedAsARoutineByItsID(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	script := `print(call_tool("read", {path="notes.txt"}) ~= nil)`
	if out, _, failed := r.call(t, "dp", "dummy_plug", map[string]any{"goal": "check the notes", "script": script}, "allow"); failed {
		t.Fatalf("dummy_plug: %q", out)
	}
	if err := r.command(t, "routine-trace"); err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`run (r[0-9a-f]{8}) check the notes`).FindStringSubmatch(r.logs.text())
	if m == nil {
		t.Fatalf("/routine-trace does not list the run: %q", r.logs.text())
	}
	if err := r.command(t, "routine-draft", "notes", "run:"+m[1]); err != nil {
		t.Fatal(err)
	}
	body := r.skill(t, "notes")
	for _, want := range []string{"kind: routine", "tools: read", "source: run " + m[1], script} {
		if !strings.Contains(body, want) {
			t.Fatalf("saved run lacks %q:\n%s", want, body)
		}
	}
	out, asks, failed := r.call(t, "n1", "run_routine", map[string]any{"name": "notes"}, "allow")
	if failed || len(asks) != 1 || !strings.Contains(out, "true") {
		t.Fatalf("the saved run asks once and runs: %q %+v", out, asks)
	}
}

const writerRoutine = "---\nkind: routine\ndescription: write a note\ntools: write\ninputs: {\"type\":\"object\",\"properties\":{\"text\":{\"type\":\"string\"}},\"required\":[\"text\"]}\n---\n```lua\nlocal ok, why = call_tool(\"write\", {path=\"out.txt\", content=input.text})\nprint(ok and \"wrote\" or (\"refused: \" .. tostring(why)))\n```\n"

// A routine may write. Its approval says so, and approving it lets it run, not write: each write
// still asks as it would outside a routine, and refusing one refuses only that write.
func TestAWritingRoutineIsApprovedOnceAndEachWriteStillAsks(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	r.file(t, filepath.Join(".magi", "skills", "writer.md"), writerRoutine)
	out, asks, failed := r.call(t, "w1", "run_routine", map[string]any{"name": "writer", "args": map[string]any{"text": "hi"}}, "allow")
	if failed || len(asks) != 2 || asks[0].ID != "w1/approve" || !strings.Contains(string(asks[0].Args), `"writes":["write"]`) || asks[1].What != "write" || !strings.Contains(out, "wrote") {
		t.Fatalf("first run: approval naming the writes, then the write's own prompt: %q %+v", out, asks)
	}
	if b, _ := os.ReadFile(filepath.Join(r.s.Workdir, "out.txt")); string(b) != "hi" {
		t.Fatalf("out.txt = %q", b)
	}
	out, asks, _ = r.call(t, "w2", "run_routine", map[string]any{"name": "writer", "args": map[string]any{"text": "bye"}}, "deny")
	if len(asks) != 1 || asks[0].What != "write" || !strings.Contains(out, "refused: ") {
		t.Fatalf("second run: no approval, the write still asks and a refusal refuses it: %q %+v", out, asks)
	}
	if b, _ := os.ReadFile(filepath.Join(r.s.Workdir, "out.txt")); string(b) != "hi" {
		t.Fatalf("a refused write happened: out.txt = %q", b)
	}
}

// overlapTool stands in for a writing tool and measures how many of its calls ran at the same time.
type overlapTool struct {
	name         string
	active, peak int32
}

func (o *overlapTool) Name() string            { return o.name }
func (o *overlapTool) Description() string     { return "overlap probe " + o.name }
func (o *overlapTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (o *overlapTool) Execute(context.Context, json.RawMessage, port.ToolEnv) (session.ToolResult, error) {
	n := atomic.AddInt32(&o.active, 1)
	for {
		p := atomic.LoadInt32(&o.peak)
		if n <= p || atomic.CompareAndSwapInt32(&o.peak, p, n) {
			break
		}
	}
	time.Sleep(100 * time.Millisecond)
	atomic.AddInt32(&o.active, -1)
	return session.ToolResult{Content: json.RawMessage(`"ok"`)}, nil
}

const pairRoutine = "---\nkind: routine\ndescription: two writes at once\ntools: bash\ninputs: {\"type\":\"object\",\"properties\":{}}\n---\n```lua\nlocal r = call_all({ {\"bash\", {command=\"a\"}}, {\"bash\", {command=\"b\"}} })\nprint(r[1].ok, r[2].ok)\n```\n"

// A routine's writing calls run one at a time even when the script asks for them together (call_all)
// and the host would let them overlap: in "allow" no write waits on a prompt, so only the routine's
// serial list keeps them apart. (The routine's own approval still asks; it is a person's.)
func TestARoutinesWritingCallsNeverOverlap(t *testing.T) {
	r := newRoutineRigWith(t, t.TempDir(), routineFile, Config{Permission: "allow", Interactive: true, AnswerWait: 5 * time.Second})
	probe := &overlapTool{name: "bash"}
	r.a.tools.Register(probe)
	r.file(t, filepath.Join(".magi", "skills", "pair.md"), pairRoutine)
	out, asks, failed := r.call(t, "p1", "run_routine", map[string]any{"name": "pair"}, "allow")
	if failed || len(asks) != 1 || !strings.Contains(out, "true\ttrue") {
		t.Fatalf("the routine must be approved once and run both writes: %q %+v", out, asks)
	}
	if p := atomic.LoadInt32(&probe.peak); p != 1 {
		t.Fatalf("a routine's writes overlapped: %d ran at once", p)
	}
}

const graphPairRoutine = "---\nkind: routine\ndescription: two writes in a graph with no order between them\ntools: bash\ninputs: {\"type\":\"object\",\"properties\":{}}\n---\n```lua\nlocal r = call_graph({ {id=\"a\", tool=\"bash\", args={command=\"a\"}}, {id=\"b\", tool=\"bash\", args={command=\"b\"}} })\nprint(r.a ~= nil, r.b ~= nil)\n```\n"

// The same for call_graph: two writing tasks with no edge between them are ready together, and the
// graph would run them side by side; a routine's serial list keeps them one at a time.
func TestARoutinesWritingGraphTasksNeverOverlap(t *testing.T) {
	r := newRoutineRigWith(t, t.TempDir(), routineFile, Config{Permission: "allow", Interactive: true, AnswerWait: 5 * time.Second})
	probe := &overlapTool{name: "bash"}
	r.a.tools.Register(probe)
	r.file(t, filepath.Join(".magi", "skills", "gpair.md"), graphPairRoutine)
	out, asks, failed := r.call(t, "g1", "run_routine", map[string]any{"name": "gpair"}, "allow")
	if failed || len(asks) != 1 || !strings.Contains(out, "true\ttrue") {
		t.Fatalf("the routine must be approved once and run both graph writes: %q %+v", out, asks)
	}
	if p := atomic.LoadInt32(&probe.peak); p != 1 {
		t.Fatalf("a routine's graph writes overlapped: %d ran at once", p)
	}
}
