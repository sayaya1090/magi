package app

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"runtime"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/platform"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

func TestRoutineKeepsSingleCallFollowupsQuiet(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires POSIX shell routines")
	}
	r := newRoutineRig(t, t.TempDir(), routineFile)
	suggested := func(prompt string, sid session.SessionID) bool {
		t.Helper()
		hints, err := r.a.contextProviders[0].Provide(context.Background(), port.ContextQuery{
			SessionID: sid, Workdir: r.s.Workdir, Prompt: prompt,
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range hints {
			if strings.Contains(h.Text, "— run_routine") {
				return true
			}
		}
		return false
	}
	for i := range 4 {
		r.fire("user_message", "text", fmt.Sprintf("collection%d", i))
		for j := range 4 {
			raw := fmt.Sprintf(`{"command":"cat input%d.txt | sort | uniq -c"}`, j)
			r.fire("tool_finished", "tool", "bash", "args", raw, "ok", "true", "call_id", fmt.Sprintf("%d-%d", i, j))
		}
		r.fire("turn_finished")
		r.fire("turn_finished")
		if got := suggested("continue", r.sid); got {
			t.Fatalf("after %d independent requests, got suggestion %t", i+1, got)
		}
	}
	countNotes := func() int {
		events, err := r.a.store.Read(context.Background(), r.sid, 0)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, e := range events {
			if e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorSystem && strings.Contains(string(e.Data), "[routine] Suggested:") {
				count++
			}
		}
		return count
	}
	if countNotes() != 0 {
		t.Fatal("context preview posted a chat note")
	}
	r.fire("user_message", "text", "continue")
	if suggested("continue", r.sid) || suggested("process collection2 please", r.sid) ||
		suggested("redesign navigation", r.sid) || suggested("continue", session.SessionID("another")) {
		t.Fatal("follow-up context did not respect the request and session")
	}
	if suggested("continue", r.sid) || countNotes() != 0 {
		t.Fatal("single-call wrappers must not generate chat notes")
	}
	r.fire("tool_finished", "tool", "bash", "args", `{"command":"pwd"}`, "ok", "true", "call_id", "different")
	r.fire("turn_finished")
	r.fire("user_message", "text", "collection4")
	r.fire("tool_finished", "tool", "bash", "args", `{"command":"cat other.txt | sort | uniq -c"}`, "ok", "true", "call_id", "return")
	r.fire("turn_finished")
	if suggested("continue", r.sid) {
		t.Fatal("a single return to a workflow after a different shell task triggered a suggestion")
	}
	for range 8 {
		r.fire("turn_finished")
		r.fire("user_message", "text", "redesign navigation")
	}
	if suggested("continue", r.sid) {
		t.Fatal("stale routine suggested after eight unrelated requests")
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
	// Every alphabetically earlier description mentions "unique"; an exact
	// name must still be reachable within the output limit.
	for i := range 31 {
		r.file(t, filepath.Join(".magi", "skills", fmt.Sprintf("sample-%02d.md", i)), strings.Replace(routineFile, "read a file and say so", "List unique records", 1))
	}
	r.file(t, filepath.Join(".magi", "skills", "unique.md"), routineFile)
	out, _, failed := r.call(t, "lookup-exact", "routine_catalog", map[string]any{"query": "UNIQUE"}, "allow")
	var results []struct{ Name string }
	if failed || json.Unmarshal([]byte(out), &results) != nil || len(results) != 30 || results[0].Name != "unique" {
		t.Fatalf("exact routine name lost among description matches: %s", out)
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
	if err != nil || len(hints) != 0 {
		t.Fatalf("single Bash wrapper was recommended: %v %v", hints, err)
	}
	name := filepath.Base(filepath.Dir(files[0]))
	file := "today's records.txt"
	r.file(t, file, "pear\napple\npear\n")
	out, asks, failed := r.call(t, "count-records", "run_routine", map[string]any{
		"name": name, "args": map[string]string{"s1_arg1": file},
	}, "allow")
	if failed || len(asks) != 1 || asks[0].What != "bash" || !strings.Contains(out, "1 apple") || !strings.Contains(out, "2 pear") {
		t.Fatalf("pipeline output %q approvals %+v", out, asks)
	}
}

func TestRoutineKeepsSingleShellFileFlowQuiet(t *testing.T) {
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
	if err != nil || len(hints) != 0 {
		t.Fatalf("one shell call prompted a recommendation: %v %v", hints, err)
	}

}

func TestRoutineFindsLineFiltersByPurpose(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX pipeline")
	}
	r := newRoutineRig(t, t.TempDir(), routineFile)
	r.a.plat = platform.OS{}
	for _, file := range []string{"a.txt", "b.txt"} {
		r.fire("user_message", "text", "inspect files")
		raw, _ := json.Marshal(map[string]string{"command": "git diff --no-index -- empty " + file + " | grep -E feature | grep -v obsolete"})
		r.fire("tool_finished", "tool", "bash", "args", string(raw), "ok", "true", "order_known", "true")
	}
	query := "변경 내역에서 원하는 줄만 추려줘"
	hints, err := r.a.contextProviders[0].Provide(context.Background(), port.ContextQuery{SessionID: r.sid, Workdir: r.s.Workdir, Prompt: query})
	if err != nil || len(hints) != 0 {
		t.Fatalf("purpose hint: %+v (%v)", hints, err)
	}
	for _, q := range []string{"파일을 추려줘", "커밋을 보여줘"} {
		hints, err := r.a.contextProviders[0].Provide(context.Background(), port.ContextQuery{SessionID: r.sid, Workdir: r.s.Workdir, Prompt: q})
		if err != nil || len(hints) != 0 {
			t.Fatalf("unexpected hint for %q: %+v (%v)", q, hints, err)
		}
	}
	out, _, failed := r.call(t, "lookup", "routine_catalog", map[string]any{"query": query}, "allow")
	var entries []struct {
		Name   string
		Inputs struct {
			Properties map[string]struct{ Description string }
		}
	}
	if failed || json.Unmarshal([]byte(out), &entries) != nil || len(entries) != 1 {
		t.Fatal(out)
	}
	args := map[string]string{"s1_arg4": "empty", "s1_arg5": "fresh"}
	for key, prop := range entries[0].Inputs.Properties {
		switch prop.Description {
		case "extended regular expression for lines to keep":
			args[key] = "feature"
		case "basic regular expression for lines to exclude":
			args[key] = "obsolete"
		}
	}
	r.file(t, "empty", "")
	r.file(t, "fresh", "feature new\nfeature obsolete\nunrelated\n")
	out, _, failed = r.call(t, "apply", "run_routine", map[string]any{"name": entries[0].Name, "args": args}, "allow")
	if failed || !strings.Contains(out, "+feature new") || strings.Contains(out, "feature obsolete") || strings.Contains(out, "unrelated") {
		t.Fatal(out)
	}
}

func TestRoutineRecallsBoundShellAfterMeasuredSavings(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell pipeline")
	}
	r := newRoutineRig(t, t.TempDir(), routineFile)
	const request = "summarize sorted records"
	fire := func(tool, args string, tokens int, id string) {
		r.fire("user_message", "text", request)
		r.fire("tool_finished", "tool", tool, "args", args, "ok", "true", "order_known", "true", "call_id", id, "authored_model", "measured", "authored_tokens", fmt.Sprint(tokens))
	}
	const direct = `{"command":"pwd | sort | uniq"}`
	for i := range 4 {
		fire("bash", direct, 35, fmt.Sprint(i))
	}
	files, _ := filepath.Glob(filepath.Join(r.s.Workdir, ".magi/routines/write-*/routine.md"))
	if len(files) != 1 {
		t.Fatalf("expected one measured pipeline: %v", files)
	}
	name := filepath.Base(filepath.Dir(files[0]))
	args, _ := json.Marshal(map[string]string{"name": name})
	for i := range 2 {
		fire("run_routine", string(args), 15, fmt.Sprintf("wrapper-%d", i))
	}
	for _, tc := range []struct {
		model, prompt string
		want          bool
	}{{"measured", request, true}, {"other", request, false}, {"measured", "other request", false}} {
		hints, err := r.a.contextProviders[0].Provide(context.Background(), port.ContextQuery{SessionID: r.sid, Workdir: r.s.Workdir, Prompt: tc.prompt, Model: tc.model})
		if err != nil || (len(hints) > 0) != tc.want || (tc.want && !strings.Contains(hints[0].Text, name)) {
			t.Fatalf("%+v: %v %v", tc, hints, err)
		}
	}
}
