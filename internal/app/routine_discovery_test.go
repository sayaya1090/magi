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
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/platform"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

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
	if definition := r.skill(t, m[1]); !strings.Contains(definition, "status: learned-write\n") {
		t.Fatalf("expected an automatically saved shell routine with learned-write status, got %s", definition)
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
		r.fire("tool_finished", "tool", "bash", "args", string(args), "ok", "true", "order_known", "true", "call_id", fmt.Sprint(i))
	}
	files, err := filepath.Glob(filepath.Join(r.s.Workdir, ".magi", "routines", "write-*", "routine.md"))
	if err != nil || len(files) != 2 {
		t.Fatalf("expected just the two input layouts: %v (%v)", files, err)
	}
	for i := range 31 {
		body := strings.Replace(routineFile, "read a file and say so", "prepare marker together or check independent prerequisite", 1)
		r.file(t, filepath.Join(".magi", "skills", fmt.Sprintf("reference-%02d.md", i)), body)
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
		if failed || json.Unmarshal([]byte(catalog), &results) != nil || len(results) != 30 || results[0].Name != fields[1] {
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

func TestRoutineLearnsGlobResultLoopAcrossDifferentCounts(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	observe := func(size int, full bool) {
		r.fire("user_message", "text", "read matching files")
		var paths []string
		for i := range size {
			paths = append(paths, fmt.Sprintf("old/%d.txt", i))
		}
		body, _ := json.Marshal(paths)
		r.fire("tool_finished", "tool", "glob", "ok", "true", "order_known", "true", "glob_items", observedGlobItems(r.s.Workdir, session.ToolResult{Content: body}))
		if !full {
			paths = paths[:size-1]
		}
		for _, path := range paths {
			raw, _ := json.Marshal(map[string]string{"path": path})
			r.fire("tool_finished", "tool", "read", "args", string(raw), "order_known", "true", "ok", "true")
		}
		r.fire("turn_finished")
	}
	name := func() string {
		files, _ := filepath.Glob(filepath.Join(r.s.Workdir, ".magi/routines/auto-*/routine.md"))
		for _, f := range files {
			n := filepath.Base(filepath.Dir(f))
			if strings.Contains(r.skill(t, n), "Read every file returned by glob") {
				return n
			}
		}
		return ""
	}
	observe(3, true)
	observe(5, false)
	observe(3, true)
	if name() != "" {
		t.Fatal("partial consumption or a fixed count was treated as a variable loop")
	}
	observe(5, true)
	n := name()
	if n == "" {
		t.Fatal("two complete observations at lengths 3 and 5 did not produce a routine")
	}
	r.put(t, "docs/a.txt", []byte("first"))
	r.put(t, "docs/b.txt", []byte("second"))
	r.put(t, "docs/c.md", []byte("excluded"))
	args := map[string]any{"name": n, "args": map[string]any{"search": map[string]string{"path": "docs", "pattern": "*.txt"}}}
	out, asks, failed := r.call(t, "new-size", "run_routine", args, "allow")
	if failed || len(asks) == 0 || !strings.Contains(out, "first") || !strings.Contains(out, "second") || strings.Contains(out, "excluded") {
		t.Fatalf("new result set: %q, %v", out, asks)
	}
	args["args"] = map[string]any{"search": map[string]string{"path": "docs", "pattern": "*.missing"}}
	out, asks, failed = r.call(t, "empty-search", "run_routine", args, "allow")
	if failed || len(asks) == 0 || strings.Contains(out, "first") || strings.Contains(out, "second") {
		t.Fatalf("changed search with zero matches: %q, %v", out, asks)
	}
}
