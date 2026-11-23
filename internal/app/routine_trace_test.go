package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/platform"
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
	b, err := os.ReadFile(filepath.Join(r.s.Workdir, ".magi", "routines", name, "routine.md"))
	if os.IsNotExist(err) {
		b, err = os.ReadFile(filepath.Join(r.s.Workdir, ".magi", "skills", name+".md"))
	}
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
	r.file(t, filepath.Join(".magi", "routines", "finder", "routine.md"), strings.Replace(body, "status: draft\n", "", 1))
	out, asks, failed = r.call(t, "f2", "run_routine", map[string]any{"name": "finder", "args": args}, "allow")
	if failed || len(asks) != 1 || !strings.Contains(out, "done: 3 steps") {
		t.Fatalf("once reviewed, the routine asks once and runs: %q %+v", out, asks)
	}
}

func TestRepeatedWritesAndShellOperationsBecomeReusable(t *testing.T) {
	dataDir := t.TempDir()
	r := newRoutineRig(t, dataDir, routineFile)
	observe := func(tool, args string, readsBefore, readsAfter int) {
		r.fire("user_message")
		for range readsBefore {
			r.fire("tool_finished", "tool", "read", "ok", "true", "order_known", "true")
		}
		r.fire("tool_finished", "tool", tool, "args", args, "ok", "true", "order_known", "true")
		for range readsAfter {
			r.fire("tool_finished", "tool", "read", "ok", "true", "order_known", "true")
		}
		r.fire("turn_finished")
	}
	drafts := func() []string {
		t.Helper()
		files, err := filepath.Glob(filepath.Join(r.s.Workdir, ".magi", "routines", "write-*", "routine.md"))
		if err != nil {
			t.Fatal(err)
		}
		return files
	}
	observe("bash", `{"command":"mkdir -p private-first"}`, 1, 0)
	r.fire("turn_finished")
	observe("bash", `{"command":"touch private-second"}`, 0, 0)
	if files := drafts(); len(files) != 0 {
		t.Fatalf("different commands or duplicate turn events counted as repetition: %v", files)
	}
	r.fire("user_message")
	for _, command := range []string{"mkdir -p private-third", "mkdir -p private-fourth"} {
		r.fire("tool_finished", "tool", "bash", "args", fmt.Sprintf(`{"command":%q}`, command), "ok", "true", "order_known", "true")
		r.fire("tool_finished", "tool", "read", "ok", "true", "order_known", "true")
		r.fire("tool_finished", "tool", "todowrite", "ok", "true", "order_known", "true")
	}
	if files := drafts(); len(files) != 2 {
		t.Fatalf("expected mkdir and mkdir→read to be learned before turn_finished: %v", files)
	}
	observe("write", `{"path":"private-first","content":"private-content"}`, 0, 0)
	observe("write", `{"path":"private-second","content":"private-content"}`, 0, 0)
	files := drafts()
	if len(files) != 3 {
		t.Fatalf("expected routines for mkdir, mkdir→read, and write: %v", files)
	}
	hints, err := r.a.contextProviders[0].Provide(context.Background(), port.ContextQuery{
		SessionID: r.sid, Workdir: r.s.Workdir, Prompt: "Create a file.",
	})
	if err != nil || len(hints) != 1 || !strings.Contains(hints[0].Text, "Writing sequence: write") {
		t.Fatalf("learned writing routine was not suggested: %+v (%v)", hints, err)
	}
	store, err := os.ReadFile(filepath.Join(dataDir, "plugin-data", "dummy-plug.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		name := filepath.Base(filepath.Dir(file))
		body := r.skill(t, name)
		if !strings.Contains(body, "status: learned-write") || !strings.Contains(body, "dry_check: passed ") {
			t.Fatalf("learned routine lacks status or dry check: %s", body)
		}
		if strings.Contains(body+string(store)+r.logs.text(), "private-") {
			t.Fatal("writing observation retained an original argument")
		}
		catalog, _, failed := r.call(t, "catalog-"+name, "routine_catalog", nil, "allow")
		if failed || !strings.Contains(catalog, name) {
			t.Fatalf("learned routine is missing from the catalog: %s", catalog)
		}
		if strings.Contains(body, "tools: write\n") {
			args := map[string]any{"name": name, "args": map[string]any{
				"step1": map[string]string{"path": "out.txt", "content": "current request"},
			}}
			out, asks, _ := r.call(t, "learned-deny", "run_routine", args, "deny")
			if len(asks) != 1 || asks[0].What != "write" || !strings.Contains(out, "denied by user") {
				t.Fatalf("expected only the write permission check: %q %+v", out, asks)
			}
			if _, err := os.Stat(filepath.Join(r.s.Workdir, "out.txt")); !os.IsNotExist(err) {
				t.Fatalf("refused write changed the workspace: %v", err)
			}
			out, asks, failed = r.call(t, "learned-allow", "run_routine", args, "allow")
			if failed || len(asks) != 1 || asks[0].What != "write" || !strings.Contains(out, "done: 1 steps") {
				t.Fatalf("expected the permitted write to run: %q %+v", out, asks)
			}
			r.file(t, filepath.Join(".magi", "routines", name, "routine.md"), body+"\n")
			out, asks, failed = r.call(t, "learned-modified", "run_routine", args, "deny")
			if !failed || len(asks) != 1 || asks[0].ID != "learned-modified/approve" {
				t.Fatalf("modified file inherited generated privileges: %q %+v", out, asks)
			}
		}
	}
}

func TestShellRoutineLearnsUnlistedCommands(t *testing.T) {
	dataDir := t.TempDir()
	r := newRoutineRig(t, dataDir, routineFile)
	r.fire("user_message")
	for _, command := range []string{
		"MODE=private-alpha project-checker validate --profile 'private | alpha' | result-view\nreport",
		"project-checker validate --profile 'private beta' && result-view",
		"MODE=private-gamma project-checker validate --profile 'private gamma' | result-view; report",
	} {
		args, err := json.Marshal(map[string]string{"command": command})
		if err != nil {
			t.Fatal(err)
		}
		r.fire("tool_finished", "tool", "bash", "args", string(args), "ok", "true", "order_known", "true")
	}
	hints, err := r.a.contextProviders[0].Provide(context.Background(), port.ContextQuery{
		SessionID: r.sid, Workdir: r.s.Workdir, Prompt: "project-checker 다시 돌려줘",
	})
	if err != nil || len(hints) != 1 || strings.Count(hints[0].Text, "bash (project-checker | result-view ; report)") != 1 {
		t.Fatalf("expected a single matching shell routine: %+v (%v)", hints, err)
	}
	store, err := os.ReadFile(filepath.Join(dataDir, "plugin-data", "dummy-plug.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(store), "private ") || strings.Contains(string(store), "private-") {
		t.Fatal("shell trace stored an operand")
	}
}

func TestRoutineMatchesShellDependencyAcrossDifferentScripts(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	r.fire("user_message")
	for i, command := range []string{
		"prepare; compiler first.go && checker first.log > first-report && summarize first-report",
		"compiler second.go | checker second.log > second-report | summarize second-report",
		"compiler third.go && checker third.log > third-report && summarize third-report; cleanup",
		"compiler fourth.go | checker fourth.log > fourth-report | summarize fourth-report",
	} {
		args, err := json.Marshal(map[string]string{"command": command})
		if err != nil {
			t.Fatal(err)
		}
		r.fire("tool_finished", "tool", "bash", "args", string(args), "ok", "true", "order_known", "true", "call_id", fmt.Sprint(i))
	}
	hints, err := r.a.contextProviders[0].Provide(context.Background(), port.ContextQuery{
		SessionID: r.sid, Workdir: r.s.Workdir, Prompt: "Run compiler, checker, and summarize",
	})
	if err != nil || len(hints) != 1 || !strings.Contains(hints[0].Text, "compiler && checker && summarize") ||
		strings.Contains(hints[0].Text, "compiler && checker\n") {
		t.Fatalf("expected only the repeated dependency graph: %+v (%v)", hints, err)
	}
}

func TestRoutineLearnsAndExecutesAConditionalWorkspaceTask(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the recorded workflow requires a POSIX shell")
	}
	r := newRoutineRig(t, t.TempDir(), routineFile)
	r.a.plat = platform.OS{}
	// Successful status belongs to the shell as a whole. A function definition
	// and the unused side of an OR-list can both contain the same chain.
	for i, command := range []string{
		"unused() { mkdir -p alpha && touch alpha/ready && test -f alpha/ready; }",
		"true || { mkdir -p beta && touch beta/done && test -f beta/done; }",
	} {
		raw, err := json.Marshal(map[string]string{"command": command})
		if err != nil {
			t.Fatal(err)
		}
		r.fire("tool_finished", "tool", "bash", "args", string(raw), "ok", "true", "call_id", fmt.Sprintf("unused-%d", i))
	}
	files, err := filepath.Glob(filepath.Join(r.s.Workdir, ".magi", "routines", "write-*", "routine.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.Contains(r.skill(t, filepath.Base(filepath.Dir(file))), "Conditional shell workflow:") {
			t.Fatal("learned an executable workflow from two unexecuted branches")
		}
	}
	for i, command := range []string{
		`pwd; mkdir -p alpha && touch "alpha/ready" && test -f alpha/ready`,
		`mkdir -p beta && touch beta/done && test -f 'beta/done' && pwd`,
	} {
		r.fire("user_message", "text", fmt.Sprintf("작업공간 준비 %d", i))
		raw, err := json.Marshal(map[string]string{"command": command})
		if err != nil {
			t.Fatal(err)
		}
		r.fire("tool_finished", "tool", "bash", "args", string(raw), "ok", "true", "call_id", fmt.Sprint(i))
	}
	hints, err := r.a.contextProviders[0].Provide(context.Background(), port.ContextQuery{
		SessionID: r.sid, Workdir: r.s.Workdir, Prompt: "이번 작업공간 준비 부탁해",
	})
	if err != nil || len(hints) != 1 {
		t.Fatalf("expected learned workflow for the next request: %+v %v", hints, err)
	}
	m := regexp.MustCompile(`run_routine (write-[0-9a-f]+) inputs`).FindStringSubmatch(hints[0].Text)
	if len(m) != 2 {
		t.Fatalf("hint is missing an executable routine: %s (%s)", hints[0].Text, r.logs.text())
	}
	dir := "Sam's report"
	args := map[string]any{"name": m[1], "args": map[string]string{"s1_arg2": dir, "s2_arg1": dir + "/done"}}
	_, asks, _ := r.call(t, "deny-chain", "run_routine", args, "deny")
	if len(asks) != 1 || asks[0].What != "bash" {
		t.Fatalf("expected the inner shell permission gate: %+v", asks)
	}
	if _, err := os.Stat(filepath.Join(r.s.Workdir, dir)); !os.IsNotExist(err) {
		t.Fatalf("denied chain changed the workspace: %v", err)
	}
	output, _, failed := r.call(t, "allowed-chain", "run_routine", args, "allow")
	if failed {
		t.Fatal(output)
	}
	if _, err := os.Stat(filepath.Join(r.s.Workdir, dir, "done")); err != nil {
		t.Fatalf("the workflow did not produce the requested file: %v (%s)", err, output)
	}
}

func TestRoutineRemembersAlternatingInputDependencies(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires POSIX shell workflows")
	}
	r := newRoutineRig(t, t.TempDir(), routineFile)
	r.a.plat = platform.OS{}
	for i, observation := range []struct{ request, command string }{
		{"prepare marker together", "mkdir -p a && touch a/one && test -f a/one"},
		{"check independent prerequisite", "mkdir -p b && touch b/two && test -f b/three"},
		{"prepare marker together", "mkdir -p c && touch c/four && test -f c/four"},
		{"check independent prerequisite", "mkdir -p d && touch d/five && test -f d/six"},
		{"document dormant examples", "demo() { mkdir -p e && touch e/seven && test -f e/seven; }"},
	} {
		r.fire("user_message", "text", observation.request)
		args, err := json.Marshal(map[string]string{"command": observation.command})
		if err != nil {
			t.Fatal(err)
		}
		r.fire("tool_finished", "tool", "bash", "args", string(args), "ok", "true", "call_id", fmt.Sprint(i))
	}
	for _, scenario := range []struct{ request, want string }{
		{"prepare marker together", "test -f {s2_arg1}"},
		{"check independent prerequisite", "test -f {s3_arg2}"},
		{"document dormant examples", ""},
		{"mkdir touch test", ""},
	} {
		hints, err := r.a.contextProviders[0].Provide(context.Background(), port.ContextQuery{
			SessionID: r.sid, Workdir: r.s.Workdir, Prompt: scenario.request,
		})
		if err != nil || len(hints) != 1 {
			t.Fatalf("context for %q: %+v %v", scenario.request, hints, err)
		}
		text := hints[0].Text
		if strings.Contains(text, "— run_routine") != (scenario.want != "") || !strings.Contains(text, scenario.want) {
			t.Fatalf("wrong workflow for %q: %s", scenario.request, text)
		}
		if scenario.want == "" {
			continue
		}
		fields := regexp.MustCompile(`run_routine (write-[0-9a-f]+) inputs`).FindStringSubmatch(text)
		if len(fields) != 2 {
			t.Fatal(text)
		}
		catalog, _, failed := r.call(t, "find-"+fields[1], "routine_catalog", map[string]any{"query": scenario.request}, "allow")
		var results []struct{ Name string }
		if failed || json.Unmarshal([]byte(catalog), &results) != nil || len(results) != 1 || results[0].Name != fields[1] {
			t.Fatalf("catalog selected the wrong input layout for %q: %s", scenario.request, catalog)
		}
		inputs := map[string]string{"s1_arg2": "current", "s2_arg1": "current/marker"}
		separate := scenario.want == "test -f {s3_arg2}"
		if separate {
			inputs["s3_arg2"] = "current/missing-prerequisite"
		}
		out, _, failed := r.call(t, fields[1], "run_routine", map[string]any{"name": fields[1], "args": inputs}, "allow")
		if failed != separate {
			t.Fatalf("wrong file checked for %q: %s", scenario.request, out)
		}
	}
}

func TestRoutineCatalogSearchesPastItsFirstThirtyEntries(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	for i := range 31 {
		r.file(t, filepath.Join(".magi", "skills", fmt.Sprintf("sample-%02d.md", i)), routineFile)
	}
	r.file(t, filepath.Join(".magi", "skills", "z-last.md"), strings.Replace(routineFile, "read a file and say so", "Count unique records", 1))
	for _, query := range []string{"Z-LAST", "unique read"} {
		out, _, failed := r.call(t, "lookup-"+query, "routine_catalog", map[string]any{"query": query}, "allow")
		if failed || !strings.Contains(out, "z-last") || strings.Contains(out, "sample-00") {
			t.Fatalf("catalog query %q failed: %s", query, out)
		}
	}
}

func TestRoutineReplaysAPipelineWithTheNewRequestsFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX pipeline")
	}
	r := newRoutineRig(t, t.TempDir(), routineFile)
	r.a.plat = platform.OS{}
	for i := range 2 {
		r.fire("tool_finished", "tool", "bash", "ok", "true", "background", "true",
			"args", `{"command":"cat jobs.txt | sort | uniq -c","background":true}`, "call_id", fmt.Sprintf("job-%d", i))
	}
	premature, err := r.a.contextProviders[0].Provide(context.Background(), port.ContextQuery{
		SessionID: r.sid, Workdir: r.s.Workdir, Prompt: "cat sort uniq",
	})
	if err != nil || len(premature) != 0 {
		t.Fatalf("learned from launch acknowledgements: %+v (%v)", premature, err)
	}
	for i, file := range []string{"first.txt", "second.txt"} {
		r.fire("user_message", "text", "summarize repeated records "+file)
		raw, err := json.Marshal(map[string]string{"command": "cat " + file + " | sort | uniq -c"})
		if err != nil {
			t.Fatal(err)
		}
		r.fire("tool_finished", "tool", "bash", "args", string(raw), "ok", "true", "order_known", "true", "call_id", fmt.Sprint(i))
	}
	files, err := filepath.Glob(filepath.Join(r.s.Workdir, ".magi", "routines", "write-*", "routine.md"))
	if err != nil || len(files) != 1 {
		t.Fatalf("expected one pipeline routine after two complete observations: %v (%v)", files, err)
	}
	hints, err := r.a.contextProviders[0].Provide(context.Background(), port.ContextQuery{
		SessionID: r.sid, Workdir: r.s.Workdir, Prompt: "summarize repeated records from the new file",
	})
	if err != nil || len(hints) != 1 {
		t.Fatalf("missing pipeline recommendation: %+v (%v)", hints, err)
	}
	m := regexp.MustCompile(`run_routine (write-[0-9a-f]+) inputs cat \{s1_arg1\} \| sort \| uniq -c`).FindStringSubmatch(hints[0].Text)
	if len(m) != 2 {
		t.Fatal(hints[0].Text)
	}
	file := "today's records.txt"
	r.file(t, file, "pear\napple\npear\n")
	out, asks, failed := r.call(t, "count-records", "run_routine", map[string]any{
		"name": m[1], "args": map[string]string{"s1_arg1": file},
	}, "allow")
	if failed || len(asks) != 1 || asks[0].What != "bash" || !strings.Contains(out, "1 apple") || !strings.Contains(out, "2 pear") {
		t.Fatalf("pipeline output %q approvals %+v", out, asks)
	}
}

func TestRoutineSuggestsRepeatedShellFileFlow(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	r.fire("user_message")
	for _, command := range []string{
		"producer > /tmp/private-a; unrelated; checker < /tmp/private-a > /tmp/private-b; summarize < /tmp/private-b",
		"producer > /tmp/private-c; checker < /tmp/private-c > /tmp/private-d; summarize < /tmp/private-d",
	} {
		args, err := json.Marshal(map[string]string{"command": command})
		if err != nil {
			t.Fatal(err)
		}
		r.fire("tool_finished", "tool", "bash", "args", string(args), "ok", "true", "order_known", "true")
	}
	hints, err := r.a.contextProviders[0].Provide(context.Background(), port.ContextQuery{
		SessionID: r.sid, Workdir: r.s.Workdir, Prompt: "Run producer, checker, and summarize",
	})
	if err != nil || len(hints) != 1 || !strings.Contains(hints[0].Text, "producer → checker → summarize") ||
		strings.Contains(hints[0].Text, "private-") {
		t.Fatalf("expected a path-independent file flow hint: %+v (%v)", hints, err)
	}
}

func TestRoutineReusesSameFileDependencyDespiteDifferentInterveningCalls(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	for i, path := range []string{"observed-a.txt", "observed-b.txt"} {
		r.fire("user_message", "text", "릴리스 노트 작성하고 확인해 "+path)
		r.fire("tool_finished", "tool", "write", "args", fmt.Sprintf(`{"path":%q}`, path), "ok", "true", "order_known", "true")
		for range i + 1 {
			r.fire("tool_finished", "tool", "list", "ok", "true", "order_known", "true")
		}
		r.fire("tool_finished", "tool", "read", "args", fmt.Sprintf(`{"path":%q}`, path), "ok", "true", "order_known", "true")
	}
	files, err := filepath.Glob(filepath.Join(r.s.Workdir, ".magi", "routines", "write-*", "routine.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		name := filepath.Base(filepath.Dir(file))
		if !strings.Contains(r.skill(t, name), "Write and read the same file") {
			continue
		}
		hints, err := r.a.contextProviders[0].Provide(context.Background(), port.ContextQuery{
			SessionID: r.sid, Workdir: r.s.Workdir, Prompt: "다음 릴리스 노트 부탁해",
		})
		if err != nil || len(hints) != 1 || !strings.Contains(hints[0].Text, name) {
			t.Fatalf("routine absent from a new request with no tool names: %+v (%v)", hints, err)
		}
		explicit, err := r.a.contextProviders[0].Provide(context.Background(), port.ContextQuery{
			SessionID: r.sid, Workdir: r.s.Workdir, Prompt: "릴리스 노트 작성하고 파일을 다시 읽어 줘",
		})
		if err != nil || len(explicit) != 1 || !strings.Contains(explicit[0].Text, "args.step1") ||
			strings.Contains(explicit[0].Text, "Writing sequence:") {
			t.Fatalf("expected one invocation for the matched write and read: %+v (%v)", explicit, err)
		}
		params := map[string]any{"step1": map[string]string{"path": "out.txt", "content": "fresh output"},
			"step2": map[string]string{"path": "wrong.txt"}}
		args := map[string]any{"name": name, "args": params}
		out, asks, _ := r.call(t, "mismatch", "run_routine", args, "allow")
		if len(asks) != 0 || !strings.Contains(out, "must target the same file") {
			t.Fatalf("mismatched dependency was not refused: %q %+v", out, asks)
		}
		delete(params, "step2")
		out, asks, failed := r.call(t, "matching", "run_routine", args, "allow")
		if failed || len(asks) != 1 || asks[0].What != "write" ||
			!strings.Contains(out, "Step 2: read") || !strings.Contains(out, "fresh output") {
			t.Fatalf("expected write and read in one routine call: %q %+v", out, asks)
		}
		return
	}
	t.Fatal("same-file routine was not learned")
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
	if _, err := os.Stat(filepath.Join(r.s.Workdir, ".magi", "routines", automatic, "routine.md")); err != nil {
		t.Fatalf("generated routine missing from its workspace directory: %v", err)
	}
	autoBody := r.skill(t, automatic)
	if !strings.Contains(autoBody, "status: auto-review") || !strings.Contains(autoBody, "dry_check: passed 2/2") {
		t.Fatalf("automatic routine lacks a dry check: %s", autoBody)
	}
	catalog, _, failed := r.call(t, "catalog", "routine_catalog", nil, "allow")
	if failed || !strings.Contains(catalog, automatic) || !strings.Contains(catalog, "step2") {
		t.Fatalf("routine is missing from model discovery: %s", catalog)
	}
	if len(r.a.contextProviders) != 1 {
		t.Fatalf("routine context providers: %d", len(r.a.contextProviders))
	}
	provider := r.a.contextProviders[0]
	hints, err := provider.Provide(context.Background(), port.ContextQuery{SessionID: r.sid, Workdir: r.s.Workdir,
		Prompt: "Search the docs for the policy and read the README."})
	if err != nil || len(hints) != 1 || !strings.Contains(hints[0].Text, automatic) {
		t.Fatalf("no routine hint for a matching request: %+v (%v)", hints, err)
	}
	hints, err = provider.Provide(context.Background(), port.ContextQuery{SessionID: r.sid, Workdir: r.s.Workdir,
		Prompt: "Write a summary."})
	if err != nil || len(hints) != 0 {
		t.Fatalf("unexpected routine hint for an unrelated request: %+v (%v)", hints, err)
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
	if out, asks, failed := r.call(t, "new-file-auto", "run_routine", input, "deny"); !failed || len(asks) != 1 ||
		!strings.Contains(asks[0].Reason, "file:docs/nested/second.txt") || strings.Contains(out, "Step 2:") {
		t.Fatalf("an approved search or a different file granted a direct read: %s %+v", out, asks)
	}
	input["args"].(map[string]any)["step2"] = map[string]any{"path": "docs/first.txt"}
	input["reuse_read_directory"] = true
	if out, asks, failed := r.call(t, "folder-auto", "run_routine", input, "allow"); failed || len(asks) != 1 ||
		!strings.Contains(asks[0].Reason, "dir:docs") {
		t.Fatalf("folder reuse did not request separate approval: %s %+v", out, asks)
	}
	delete(input, "reuse_read_directory")
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
	r.put(t, ".magi/routines/auto-t00000001/routine.md", []byte(base))
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

func TestAParallelReaderRoutineIsSavedWithinOneTurn(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	r.put(t, "docs/guide.txt", []byte("hello guide\n"))
	routines := filepath.Join(r.s.Workdir, ".magi", "routines")
	finished := func(batch, call, tool, size, ok string) {
		t.Helper()
		r.fire("tool_finished", "parallel_batch", batch, "parallel_size", size, "call_id", call, "tool", tool, "ok", ok)
	}
	r.fire("user_message", "text", "Read the guide and search the docs")
	finished("incomplete", "missing-1", "read", "2", "true")
	finished("failed", "failed-1", "grep", "2", "true")
	finished("failed", "failed-2", "read", "2", "false")
	finished("first", "first-1", "read", "2", "true")
	finished("first", "first-2", "grep", "2", "true")
	r.fire("turn_finished")
	if files, _ := filepath.Glob(filepath.Join(routines, "auto-*", "routine.md")); len(files) != 0 {
		t.Fatalf("an incomplete or failed group counted as an observation: %v", files)
	}
	finished("second", "second-1", "grep", "2", "true")
	finished("second", "second-2", "read", "2", "true")
	r.fire("turn_finished")
	files, err := filepath.Glob(filepath.Join(routines, "auto-*", "routine.md"))
	if err != nil || len(files) != 1 {
		t.Fatalf("expected one routine for two complete groups: %v (%v)", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil || !strings.Contains(string(data), "call_all({") || !strings.Contains(string(data), "dry_check: passed 2/2") {
		t.Fatalf("parallel routine did not pass dry replay: %s (%v)", data, err)
	}
	name := filepath.Base(filepath.Dir(files[0]))
	out, asks, failed := r.call(t, "parallel-run", "run_routine", map[string]any{"name": name, "args": map[string]any{
		"step1": map[string]string{"path": "docs", "pattern": "hello"},
		"step2": map[string]string{"path": "docs/guide.txt"},
	}}, "allow")
	if failed || len(asks) != 1 || !strings.Contains(out, "Step 1: grep") || !strings.Contains(out, "Step 2: read") {
		t.Fatalf("parallel reader routine failed: %s (%d approvals)", out, len(asks))
	}

	r.fire("user_message", "text", "Search the docs for hello and guide.")
	for _, batch := range []string{"grep-first", "grep-second"} {
		finished(batch, batch+"-1", "grep", "2", "true")
		finished(batch, batch+"-2", "grep", "2", "true")
	}
	r.fire("turn_finished")
	hints, err := r.a.contextProviders[0].Provide(context.Background(), port.ContextQuery{
		SessionID: r.sid, Workdir: r.s.Workdir, Prompt: "Search the docs for hello and guide.",
	})
	if err != nil || len(hints) != 1 || !strings.Contains(hints[0].Text, "Parallel readers: grep, grep") || strings.Contains(hints[0].Text, name) {
		t.Fatalf("expected only the matching grep routine: %+v (%v)", hints, err)
	}
}
