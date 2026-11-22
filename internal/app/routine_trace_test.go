package app

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestAStretchOfTheTraceBecomesARoutineDraftCheckedDry(t *testing.T) {
	dataDir := t.TempDir()
	r := newRoutineRig(t, dataDir, routineFile)
	r.file(t, "abc1234.txt", "a\n")
	r.file(t, "deadbeef9.txt", "d\n")
	r.fire("user_message", "text", "read abc1234.txt for me")
	r.fire("tool_finished", "order_known", "true", "tool", "read", "args", `{"path":"notes.txt"}`, "ok", "true")
	r.fire("tool_finished", "order_known", "true", "tool", "read", "args", `{"path":"abc1234.txt"}`, "ok", "true")
	r.fire("tool_finished", "order_known", "true", "tool", "read", "args", `{"path":"deadbeef9.txt"}`, "ok", "true")
	if err := r.command(t, "routine-trace"); err != nil || !strings.Contains(r.logs.text(), "#3 turn 1 read") {
		t.Fatalf("/routine-trace: %v %q", err, r.logs.text())
	}
	if err := r.command(t, "routine-draft", "finder", "1-3"); err != nil {
		t.Fatal(err)
	}
	body := r.skill(t, "finder")
	for _, want := range []string{"kind: routine", "status: draft", "tools: read", "dry_check: passed 3/3",
		"synthetic arguments", "-- TODO: supply arguments for step 2", "input.step1", "input.step3", "source: trace "} {
		if !strings.Contains(body, want) {
			t.Fatalf("draft lacks %q:\n%s", want, body)
		}
	}
	store, err := os.ReadFile(filepath.Join(dataDir, "plugin-data", "dummy-plug.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"notes.txt", "abc1234", "deadbeef9", "for me"} {
		for _, output := range []string{body, r.logs.text(), string(store)} {
			if strings.Contains(output, secret) {
				t.Fatalf("trace copied %q into %s", secret, output)
			}
		}
	}
	args := map[string]any{"step1": map[string]string{"path": "notes.txt"},
		"step2": map[string]string{"path": "abc1234.txt"}, "step3": map[string]string{"path": "deadbeef9.txt"}}
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

func TestARepeatedStretchOfTheTraceIsOfferedThroughTheSameDraftPath(t *testing.T) {
	dataDir := t.TempDir()
	r := newRoutineRig(t, dataDir, routineFile)
	r.file(t, "a1b2c3d4.txt", "a\n")
	r.file(t, "f9e8d7c6.txt", "f\n")
	turn := func(file string) {
		r.fire("user_message", "text", "open "+file)
		r.fire("tool_finished", "order_known", "true", "tool", "grep", "args", `{"path":"notes.txt","pattern":"hello"}`, "ok", "true")
		r.fire("tool_finished", "order_known", "true", "tool", "read", "args", `{"path":"`+file+`"}`, "ok", "true")
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
	automatic := "auto-" + m[1]
	autoBody := r.skill(t, automatic)
	if !strings.Contains(autoBody, "status: auto-review") || !strings.Contains(autoBody, "dry_check: passed 2/2") {
		t.Fatalf("automatic routine lacks a dry check: %s", autoBody)
	}
	catalog, _, failed := r.call(t, "catalog", "routine_catalog", nil, "allow")
	if failed || !strings.Contains(catalog, automatic) || !strings.Contains(catalog, "step2") {
		t.Fatalf("routine is missing from model discovery: %s", catalog)
	}
	r.put(t, "docs/first.txt", []byte("hello first"))
	r.put(t, "docs/nested/second.txt", []byte("hello second"))
	r.put(t, "other/third.txt", []byte("hello third"))
	_ = os.Symlink("../other/third.txt", filepath.Join(r.s.Workdir, "docs", "linked.txt"))
	input := map[string]any{"name": automatic, "args": map[string]any{
		"step1": map[string]any{"path": "docs", "pattern": "hello"},
		"step2": map[string]any{"path": "docs/first.txt"},
	}}
	if out, asks, failed := r.call(t, "deny-auto", "run_routine", input, "deny"); !failed || len(asks) != 1 || strings.Contains(out, "Step 1:") {
		t.Fatalf("routine ran without approval: %s", out)
	}
	if out, asks, failed := r.call(t, "allow-auto", "run_routine", input, "allow"); failed || len(asks) != 1 ||
		!strings.Contains(out, "Step 2: read") || strings.Contains(out, "hello third") {
		t.Fatalf("routine did not execute current inputs: %s", out)
	}
	input["args"].(map[string]any)["step2"] = map[string]any{"path": "docs/nested/second.txt"}
	if out, asks, failed := r.call(t, "reuse-auto", "run_routine", input, "allow"); failed || len(asks) != 0 || !strings.Contains(out, "Step 2: read") {
		t.Fatalf("approved routine did not reuse its approval with new inputs: %s", out)
	}
	input["args"].(map[string]any)["step2"] = map[string]any{"path": "other/third.txt"}
	if out, asks, failed := r.call(t, "expand-auto", "run_routine", input, "deny"); !failed || len(asks) != 1 ||
		!strings.Contains(asks[0].Reason, "new read scope") || strings.Contains(out, "hello third") {
		t.Fatalf("new directory did not require approval: %s %+v", out, asks)
	}
	if err := r.command(t, "routine-candidates"); err != nil {
		t.Fatal(err)
	}
	store, err := os.ReadFile(filepath.Join(dataDir, "plugin-data", "dummy-plug.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.command(t, "routine-register", m[1], "opener"); err != nil {
		t.Fatal(err)
	}
	body := r.skill(t, "opener")
	for _, want := range []string{"kind: routine", "tools: grep, read", "dry_check: passed 2/2", "input.step1", "status: draft"} {
		if !strings.Contains(body, want) {
			t.Fatalf("registered candidate lacks %q:\n%s", want, body)
		}
	}
	for _, value := range []string{"notes.txt", "hello", "a1b2c3d4", "f9e8d7c6", "example:"} {
		if strings.Contains(body, value) || strings.Contains(autoBody, value) || strings.Contains(r.logs.text(), value) || strings.Contains(string(store), value) {
			t.Fatalf("repeated candidate copied %q", value)
		}
	}
}

func TestRoutineTraceRefusesToDraftUnknownCompletionOrder(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	for range 2 {
		r.fire("user_message")
		r.fire("tool_finished", "tool", "grep", "call_id", "b", "parallel_batch", "batch-1", "ok", "true")
		r.fire("tool_finished", "tool", "read", "call_id", "a", "parallel_batch", "batch-1", "ok", "true")
		r.fire("turn_finished")
	}
	r.fire("user_message")
	r.fire("tool_finished", "tool", "read", "ok", "true")
	r.fire("tool_finished", "tool", "grep", "ok", "true")
	if err := r.command(t, "routine-trace"); err != nil {
		t.Fatal(err)
	}
	for _, shown := range []string{"grep (parallel batch batch-1)", "read (order unknown)"} {
		if !strings.Contains(r.logs.text(), shown) {
			t.Fatalf("trace did not show %q: %s", shown, r.logs.text())
		}
	}
	if strings.Contains(r.logs.text(), "[routine] candidate") {
		t.Fatal("a parallel stretch was offered as a routine candidate")
	}
	for _, stretch := range []string{"1-2", "5-6"} {
		err := r.command(t, "routine-draft", "unordered", stretch)
		if err == nil || !strings.Contains(err.Error(), "no verified sequential order") {
			t.Fatalf("draft %s: %v", stretch, err)
		}
	}
	if err := r.command(t, "routine-draft", "single", "1"); err != nil {
		t.Fatal(err)
	}
}

func TestRoutineTraceCleansLegacyArgumentsAndCandidatePreviews(t *testing.T) {
	dataDir := t.TempDir()
	path := filepath.Join(dataDir, "plugin-data", "dummy-plug.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	legacy := `{"routine_last_session":"legacy","routine_trace:older":{"calls":[{"n":1,"tool":"read","args":"older-argument"}]},"routine_trace:legacy":{"prompts":{"1":"old-prompt"},"calls":[{"n":1,"turn":1,"tool":"read","args":"old-argument","ok":true}]},"routine_candidates":{"t123":{"kind":"trace","sequence":"read","prompt":"old-prompt","script":"old-preview","calls":[{"tool":"read","args":"old-argument","ok":true}]}}}`
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	r := newRoutineRig(t, dataDir, routineFile)
	for _, command := range []string{"routine-trace", "routine-candidates"} {
		if err := r.command(t, command); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.command(t, "routine-register", "t123", "legacy"); err != nil {
		t.Fatal(err)
	}
	store, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"old-prompt", "old-argument", "old-preview", "older-argument"} {
		for _, output := range []string{string(store), r.logs.text(), r.skill(t, "legacy")} {
			if strings.Contains(output, value) {
				t.Fatalf("legacy trace exposed %q in %s", value, output)
			}
		}
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

func TestRoutineWriteComparisonUsesIndependentFixtures(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	r.file(t, filepath.Join(".magi", "skills", "writer.md"), writerRoutine)
	r.file(t, "out.txt", "workspace original")
	out, asks, failed := r.call(t, "paired", "routine_compare_writes", map[string]any{
		"name": "writer", "args": map[string]any{"text": "baseline"},
		"files": map[string]string{"out.txt": "fixture"},
		"script": `local before = call_tool("write", {path="out.txt", content="candidate"})
if not before then error("write did not run") end
error("after writing")`,
	}, "deny")
	var report struct {
		Candidate struct {
			Error string
			Files map[string]string
		}
		DifferingFiles []string `json:"differing_files"`
		SameFileState  bool     `json:"same_file_state"`
	}
	if failed || len(asks) != 0 || json.Unmarshal([]byte(out), &report) != nil {
		t.Fatalf("comparison: %q %+v", out, asks)
	}
	if report.SameFileState || len(report.DifferingFiles) != 1 || report.DifferingFiles[0] != "out.txt" ||
		!strings.Contains(report.Candidate.Error, "after writing") || report.Candidate.Files["out.txt"] == "" {
		t.Fatalf("comparison omitted the partial write: %s", out)
	}
	body, err := os.ReadFile(filepath.Join(r.s.Workdir, "out.txt"))
	if err != nil || string(body) != "workspace original" {
		t.Fatalf("workspace changed: %q %v", body, err)
	}
	scope := filepath.Join(r.s.Workdir, "out.txt")
	out, _, failed = r.call(t, "escape", "routine_compare_writes", map[string]any{
		"name": "writer", "args": map[string]any{"text": "baseline"},
		"files":  map[string]string{},
		"script": fmt.Sprintf(`local r, e = call_tool("write", {path=%q, content="escape"}); if not r then error(e) end`, scope),
	}, "allow")
	if failed || !strings.Contains(out, "outside workdir") {
		t.Fatalf("unsafe path was not refused: %s", out)
	}
	body, err = os.ReadFile(scope)
	if err != nil || string(body) != "workspace original" {
		t.Fatalf("unsafe path changed original: %q %v", body, err)
	}
}

func TestRoutineWriteComparisonHonorsWorkspaceAbsolutePolicy(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	r.file(t, filepath.Join(".magi", "skills", "writer.md"), writerRoutine)
	protected := filepath.Join(r.s.Workdir, "out.txt")
	r.a.policy = newPolicy(nil, []string{"write(" + filepath.ToSlash(protected) + ")"}, nil)

	input := map[string]any{
		"name": "writer", "args": map[string]any{"text": "baseline"},
		"files":  map[string]string{"out.txt": "fixture"},
		"script": `local r, e = call_tool("write", {Path="./out.txt", content="candidate"}); if not r then error(e) end`,
	}
	out, _, failed := r.call(t, "fixture-policy", "routine_compare_writes", input, "allow")
	if !failed || !strings.Contains(out, "fixture path blocked by tool policy") {
		t.Fatalf("protected fixture was seeded: %q", out)
	}
	input["files"] = map[string]string{}
	out, _, failed = r.call(t, "call-policy", "routine_compare_writes", input, "allow")
	var report struct {
		Active, Candidate struct {
			Files       json.RawMessage
			FailedCalls int `json:"failed_calls"`
		}
	}
	if failed || json.Unmarshal([]byte(out), &report) != nil ||
		report.Active.FailedCalls != 1 || report.Candidate.FailedCalls != 1 ||
		string(report.Active.Files) != "[]" || string(report.Candidate.Files) != "[]" {
		t.Fatalf("protected write ran in a fixture copy: %q", out)
	}
}

func TestAutomaticRoutineLifecycleHidesEquivalentEntriesAndKeepsTheirFiles(t *testing.T) {
	dataDir := t.TempDir()
	r := newRoutineRig(t, dataDir, routineFile)
	base := strings.Replace(routineFile, "kind: routine\n", "kind: routine\nstatus: auto-review\n", 1)
	r.put(t, ".magi/skills/auto-t00000001.md", []byte(base))
	r.put(t, ".magi/skills/auto-t00000002.md", []byte(base))
	different := strings.Replace(base, "```lua\n", "```lua\n-- a different version\n", 1)
	r.put(t, ".magi/skills/auto-t00000003.md", []byte(different))
	equivalent := strings.Replace(base, "```lua\n", "```lua\n-- checked against the frozen evaluation\n", 1)
	r.put(t, ".magi/skills/auto-t00000004.md", []byte(equivalent))
	for _, name := range []string{"auto-t00000001", "auto-t00000004"} {
		dir := ".magi/routines/" + name + "/"
		r.put(t, dir+"contract.json", []byte(`{"cases":[{"id":"read","input":{"file":"notes.txt"},
		  "fixtures":[{"tool":"read","text":"hello"}],"print_contains":["read notes.txt true"]}]}`))
		r.put(t, dir+"tasks.json", []byte(`{"tasks":[
		  {"id":"dev","split":"dev","input":{"file":"notes.txt"},"fixtures":[{"tool":"read","text":"hello"}],
		   "expect":{"evidence":["read notes.txt true"]}},
		  {"id":"holdout","split":"holdout","input":{"file":"other.txt"},"fixtures":[{"tool":"read","text":"other"}],
		   "expect":{"evidence":["read other.txt true"]}}]}`))
		if err := r.command(t, "routine-evals-freeze", name); err != nil {
			t.Fatal(err)
		}
	}
	broken := strings.Replace(base, "print(\"read ", "if text ~= \"\" then error(\"lookup failed\") end\nprint(\"read ", 1)
	r.put(t, ".magi/skills/auto-t00000005.md", []byte(broken))
	for _, id := range []string{"first", "second", "third"} {
		out, _, failed := r.call(t, id, "run_routine", map[string]any{"name": "auto-t00000005", "args": map[string]any{"file": "notes.txt"}}, "allow")
		if !failed || !strings.Contains(out, "lookup failed") {
			t.Fatalf("expected an execution failure: %s", out)
		}
	}

	r.fire("idle")
	for _, skill := range r.a.Skills(r.s.Workdir) {
		if strings.HasPrefix(skill.Name, "auto-t") {
			t.Fatalf("automatic routine bypassed its catalog through the skill index: %s", skill.Name)
		}
	}
	catalog, _, failed := r.call(t, "catalog", "routine_catalog", nil, "allow")
	if failed || !strings.Contains(catalog, "auto-t00000001") || strings.Contains(catalog, "auto-t00000002") ||
		!strings.Contains(catalog, "auto-t00000003") || strings.Contains(catalog, "auto-t00000004") ||
		strings.Contains(catalog, "auto-t00000005") {
		t.Fatalf("catalog did not deduplicate only equivalent entries: %s", catalog)
	}
	if got := r.skill(t, "auto-t00000002"); got != base {
		t.Fatal("the hidden routine file changed")
	}
	if err := r.command(t, "routine-lifecycle", "restore", "auto-t00000002"); err != nil {
		t.Fatal(err)
	}
	catalog, _, failed = r.call(t, "restored", "routine_catalog", nil, "allow")
	if failed || !strings.Contains(catalog, "auto-t00000002") {
		t.Fatalf("restored entry is missing: %s", catalog)
	}
	path := filepath.Join(r.s.Workdir, ".magi/routines/auto-t00000002/decisions.jsonl")
	history, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(history), "catalog alias") || !strings.Contains(string(history), "catalog restored") {
		t.Fatalf("lifecycle decisions missing: %s (%v)", history, err)
	}
	storePath := filepath.Join(dataDir, "plugin-data", "dummy-plug.json")
	raw, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatal(err)
	}
	var store map[string]any
	if err := json.Unmarshal(raw, &store); err != nil {
		t.Fatal(err)
	}
	for key, value := range store {
		if strings.HasPrefix(key, "routine_lifecycle:") {
			state := value.(map[string]any)
			state["last_review"] = 0
			state["auto-t00000003"].(map[string]any)["since"] = time.Now().Add(-31 * 24 * time.Hour).Unix()
		}
	}
	raw, err = json.Marshal(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	r.fire("idle")
	catalog, _, failed = r.call(t, "archived", "routine_catalog", nil, "allow")
	if failed || strings.Contains(catalog, "auto-t00000003") || r.skill(t, "auto-t00000003") != different {
		t.Fatalf("unused routine was not archived intact: %s", catalog)
	}
}
