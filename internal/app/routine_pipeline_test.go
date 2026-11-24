package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/platform"
	luaplugin "github.com/sayaya1090/magi/internal/adapter/plugin/lua"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

type reviewPlannerObserver struct {
	recordingObserver
	*luaplugin.Host
}

// The same provider policy in both arms: get inventory, get the diff if it
// is missing, then finish. Count requests at the provider boundary.
type reviewProbe struct {
	inventory, diff                   string
	calls, inputEstimate, outputBytes int
	sawDiff                           bool
}

func (m *reviewProbe) StreamChat(ctx context.Context, req port.ChatRequest) (<-chan port.ProviderEvent, error) {
	m.calls++
	tools, _ := json.Marshal(req.Tools)
	m.inputEstimate += estimateTokens(req.System+string(tools), req.Messages)
	cmd := m.inventory
	for _, msg := range req.Messages {
		for _, p := range msg.Parts {
			if r := p.ToolResult; r != nil {
				if strings.Contains(string(r.Content), "+keep second") {
					m.sawDiff = true
				}
				if strings.Contains(string(r.Content), "2 files changed") {
					cmd = m.diff
				}
			}
		}
	}
	events := asideText("done")
	if !m.sawDiff {
		raw, _ := json.Marshal(map[string]string{"command": cmd})
		events = asideCall("bash", string(raw))
		events[0].ToolCall.CallID = fmt.Sprintf("review-%d", m.calls)
		m.outputBytes += len(raw)
	} else {
		m.outputBytes += len("done")
	}
	ch := make(chan port.ProviderEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)
	return ch, nil
}

// The held-out command is never fed to the learner. It needs two new paths,
// another filter, and different numeric cut/head options.
func TestPipelineRecommendationBeforeHeldOutCall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX pipeline")
	}
	r := newRoutineRig(t, t.TempDir(), routineFile)
	r.a.plat = platform.OS{}
	observe := func(id, cmd string) {
		raw, _ := json.Marshal(map[string]string{"command": cmd})
		r.fire("tool_finished", "tool", "bash", "args", string(raw), "ok", "true", "order_known", "true", "call_id", id)
	}
	const inc = " | grep -E '^[-+]' | grep -v '^+++\\|^---'"
	const tail = " | grep -v '^[-+]skip' | cut -c1-170 | head -60"
	r.fire("user_message", "text", "review the earlier change")
	observe("first", "git show --format= $c -- a"+inc+" | cut -c1-180; git show --format= $c -- b c | grep -E '^[-+@]' | grep -v '^+++\\|^---'")
	observe("second", "git show --format= earlier -- a"+inc+tail)
	r.fire("user_message", "text", "review the other commits")
	observe("third", "git show --format= $c -- d e f")
	observe("fourth", "git show --format= $c -- g"+inc)
	// Repeat both fixed-width variants and the open-subcommand pipeline.
	for i := range 2 {
		observe(fmt.Sprintf("fixed-one-%d", i), "git show --format= HEAD -- one"+inc+tail)
		observe(fmt.Sprintf("fixed-three-%d", i), "git show --format= HEAD -- one two three"+inc+" | cut -c1-180")
		observe(fmt.Sprintf("open-verb-%d", i), "git show --format='%b' HEAD | sed '/^$/d' | head -12")
	}
	files, _ := filepath.Glob(filepath.Join(r.s.Workdir, ".magi/routines/write-*/routine.md"))
	for _, path := range files {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "source: repeated shell graph") {
			t.Fatalf("redundant fixed pipeline: %s", path)
		}
	}
	events, _ := r.a.store.Read(context.Background(), r.sid, 0)
	body, _ := json.Marshal(reconstruct(events))
	if strings.Contains(string(body), "accepts current inputs") {
		t.Fatal("redundant post-execution hint")
	}
	r.a.cfg.Observer = &reviewPlannerObserver{Host: r.host}
	git := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = r.s.Workdir
		if b, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, b)
		}
	}
	git("init", "-q")
	r.file(t, "changed one", "keep first\nskip noise\n")
	r.file(t, "changed two", "keep second\n")
	git("add", "--", "changed one", "changed two")
	git("-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-qm", "fixture")
	head := exec.Command("git", "rev-parse", "--short", "HEAD")
	head.Dir = r.s.Workdir
	hash, err := head.Output()
	if err != nil {
		t.Fatal(err)
	}
	revision := strings.TrimSpace(string(hash))
	task := "review this commit"
	inventoryCommand := "git show --format='--- %h' --stat HEAD"
	rawArgs, _ := json.Marshal(map[string]string{"command": inventoryCommand})
	rawText, _ := json.Marshal("--- " + revision + "\n changed one | 2 ++\n changed two | 1 +\n 2 files changed, 3 insertions(+)\n")
	stat := resultInventory("bash", rawArgs, session.ToolResult{Content: rawText})
	var invocation struct {
		Name string
		Args map[string]any
	}
	for _, pair := range []struct {
		rev   string
		paths []string
	}{{"1111111", []string{"a", "b"}}, {"2222222", []string{"c", "d", "e"}}} {
		var text strings.Builder
		fmt.Fprintf(&text, "--- %s\n", pair.rev)
		for _, path := range pair.paths {
			fmt.Fprintf(&text, " %s | 1 +\n", path)
		}
		fmt.Fprintf(&text, " %d files changed\n", len(pair.paths))
		raw, _ := json.Marshal(text.String())
		inventory := resultInventory("bash", rawArgs, session.ToolResult{Content: raw})
		consumer, _ := json.Marshal(map[string]string{"command": "git show --format= " + pair.rev + " -- " + strings.Join(pair.paths, " ") + inc})
		r.host.InventoryRoutine(r.s.Workdir, inventory, string(consumer))
	}
	if plan := r.host.InventoryRoutine(r.s.Workdir, stat, ""); json.Unmarshal([]byte(plan), &invocation) != nil {
		t.Fatalf("missing plan %q", plan)
	}
	name := invocation.Name
	inventoryOut, _, failed := r.callTask(t, "inventory", "bash", map[string]any{"command": inventoryCommand}, "allow", task)
	if failed || !strings.Contains(inventoryOut, "2 files changed") || !strings.Contains(inventoryOut, "[inventory routine]") || !strings.Contains(inventoryOut, "+keep second") {
		t.Fatalf("inventory and diff in one response: %s", inventoryOut)
	}
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("automatic=%v", enabled), func(t *testing.T) {
			ctx := context.Background()
			sid, err := r.a.CreateSession(ctx, command.CreateSession{Workdir: r.s.Workdir})
			if err != nil {
				t.Fatal(err)
			}
			model := &reviewProbe{inventory: inventoryCommand, diff: "git show --format= HEAD -- 'changed one' 'changed two'" + inc}
			previous, observer, policy := r.a.llm, r.a.cfg.Observer, r.a.Permission()
			defer func() {
				r.a.llm, r.a.cfg.Observer = previous, observer
				r.a.SetPermission(policy)
				r.a.cfg.Interactive = true
			}()
			r.a.llm = model
			r.a.SetPermission("allow")
			r.a.cfg.Interactive = false
			want := 2
			if !enabled {
				r.a.cfg.Observer = nil
				want = 3
			}
			if err = r.a.appendPromptText(ctx, sid, event.Actor{Kind: event.ActorUser, ID: "reviewer"}, task); err != nil {
				t.Fatal(err)
			}
			_, err = r.a.runLoop(ctx, r.a.sessionInfo(ctx, sid), AgentSpec{Name: "coder"}, 0, 4, true)
			if err != nil || !model.sawDiff || model.calls != want {
				t.Fatalf("calls=%d diff=%v err=%v", model.calls, model.sawDiff, err)
			}
			t.Logf("provider requests=%d estimated input tokens=%d generated argument/text bytes=%d", model.calls, model.inputEstimate, model.outputBytes)
		})
	}
	after, _ := filepath.Glob(filepath.Join(r.s.Workdir, ".magi/routines/write-*/routine.md"))
	if len(after) != len(files) {
		t.Fatalf("saved a duplicate inventory routine: %v -> %v", files, after)
	}
	catalog, _, bad := r.call(t, "help", "routine_catalog", map[string]any{"query": name}, "allow")
	if bad || !strings.Contains(catalog, "Git revision to inspect") || !strings.Contains(catalog, "without +++/---") || !strings.Contains(catalog, "unknown tail layouts") {
		t.Fatal(catalog)
	}
	readyOut, _, readyFailed := r.call(t, "from-hint", "run_routine", map[string]any{"name": invocation.Name, "args": invocation.Args}, "allow")
	if readyFailed || !strings.Contains(readyOut, "+keep first") || !strings.Contains(readyOut, "+keep second") {
		t.Fatal(readyOut)
	}
	args := invocation.Args
	args["tail"] = [][]string{{"grep", "-v", "^[-+]skip"}, {"cut", "-c1-180"}, {"head", "-110"}}
	out, asks, failed := r.call(t, "held-out", "run_routine", map[string]any{"name": name, "args": args}, "allow")
	cmd := exec.Command("sh", "-c", "git show --format= HEAD -- 'changed one' 'changed two'"+inc+" | grep -v '^[-+]skip' | cut -c1-180 | head -110")
	cmd.Dir = r.s.Workdir
	want, err := cmd.Output()
	if err != nil || string(want) != "+keep first\n+keep second\n" || failed || len(asks) != 1 || asks[0].What != "bash" || strings.TrimSpace(strings.TrimPrefix(strings.SplitN(out, "\n[child ", 2)[0], "exit 0\n")) != strings.TrimSpace(string(want)) {
		t.Fatalf("output %q want %q asks %+v err %v", out, want, asks, err)
	}
	for k, v := range map[string]any{"s1_arg1": "status", "tail": [][]string{{"echo", "unknown"}}} {
		bad := map[string]any{}
		for key, value := range args {
			bad[key] = value
		}
		bad[k] = v
		_, asks, failed = r.call(t, fmt.Sprint("bad-", k), "run_routine", map[string]any{"name": name, "args": bad}, "allow")
		if !failed || len(asks) != 0 {
			t.Fatalf("unobserved change %s reached bash", k)
		}
	}
}

func TestLegacyShellFilterRequiresObservedSubcommand(t *testing.T) {
	data := t.TempDir()
	r := newRoutineRig(t, data, routineFile)
	r.a.plat = platform.OS{}
	name := "write-b2513010218302d2dcc3"
	body, err := os.ReadFile("testdata/legacy-shell-filter.md")
	if err != nil {
		t.Fatal(err)
	}
	r.put(t, filepath.Join(".magi", "routines", name, "routine.md"), body)
	raw, _ := json.Marshal(map[string]any{"routine_learned_write:b2513010218302d2dcc3": map[string]string{"digest": fmt.Sprintf("%x", sha256.Sum256(body)), "workspace": r.s.Workdir}})
	if err := os.MkdirAll(filepath.Join(data, "plugin-data"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "plugin-data", "dummy-plug.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	for _, verb := range []string{"status", "show"} {
		args := map[string]string{"s1_arg1": verb, "s1_arg2": "--format=", "s1_arg3": "HEAD", "s1_arg5": "go.mod", "s2_arg2": "^[-+]", "s3_arg2": "^---"}
		out, asks, failed := r.call(t, "legacy-"+verb, "run_routine", map[string]any{"name": name, "args": args}, "deny")
		if verb == "show" && len(asks) != 1 || verb == "status" && (!failed || len(asks) != 0 || !strings.Contains(out, "structure")) {
			t.Fatal(verb, asks, out)
		}
		t.Logf("old file %s: %d inner approvals", verb, len(asks))
	}
}

func TestReviewInventoryRequiresAnUnambiguousCompleteStat(t *testing.T) {
	args := json.RawMessage(`{"command":"git show --stat HEAD"}`)
	const one = "--- abc1234\n a | 1 +\n 1 file changed, 1 insertion(+)\n"
	for _, output := range []string{one + one, one + "diff --git a/a b/a\n", "--- abc1234\n a | 1 +\n 2 files changed\n"} {
		raw, _ := json.Marshal(output)
		if got := observedReviewStat(args, session.ToolResult{Content: raw}); got != "" {
			t.Fatalf("ambiguous inventory accepted: %s", got)
		}
	}
}

// The producer has no review vocabulary and the consumer is a workspace CLI.
// All three turns use different wording. The held-out list is generated afresh.
func TestInventoryLearnsUnknownConsumerFromActualCalls(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell")
	}
	r := newRoutineRigWith(t, t.TempDir(), routineFile, Config{Permission: "allow"})
	r.a.cfg.Observer = &reviewPlannerObserver{Host: r.host}
	r.a.plat = platform.OS{}
	dir := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for name, body := range map[string]string{"queue-scan": "cat queue.json", "bundle-labels": "printf 'bundle %s\\n' \"$@\""} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	for i, x := range []struct {
		prompt  string
		items   []string
		scope   string
		partial bool
	}{
		{"what is queued?", []string{"one", "two", "three"}, "batchA", true},
		{"다음 묶음을 정리해", []string{"four", "five"}, "batchB", false},
		{"assemble this set", []string{"six", "seven", "eight"}, "batchC", false},
		{"새 묶음 부탁해", []string{"nine", "ten", "eleven", "twelve"}, "batchD", false},
	} {
		payload, _ := json.Marshal(map[string]any{"tickets": x.items, "cycle": x.scope})
		r.file(t, "queue.json", string(payload))
		ids := x.items
		if x.partial {
			ids = ids[:1]
		}
		scan, _ := json.Marshal(map[string]string{"command": fmt.Sprintf("queue-scan -- job%d", i)})
		steps := [][]port.ProviderEvent{asideCall("bash", string(scan))}
		if i < 3 {
			raw, _ := json.Marshal(map[string]string{"command": "bundle-labels --cycle " + x.scope + " -- " + strings.Join(ids, " ") + " | cat"})
			next := asideCall("bash", string(raw))
			next[0].ToolCall.CallID = "use-list"
			peer := asideCall("bash", `{"command":"true"}`)[0]
			peer.ToolCall.CallID = "unrelated"
			steps = append(steps, append([]port.ProviderEvent{peer}, next...))
		}
		steps = append(steps, asideText("done"))
		r.a.llm = &scriptLLM{steps: steps}
		sid, err := r.a.CreateSession(ctx, command.CreateSession{Workdir: r.s.Workdir})
		if err != nil {
			t.Fatal(err)
		}
		if err = r.a.appendPromptText(ctx, sid, event.Actor{Kind: event.ActorUser, ID: "u"}, x.prompt); err != nil {
			t.Fatal(err)
		}
		if _, err = r.a.runLoop(ctx, r.a.sessionInfo(ctx, sid), AgentSpec{Name: "coder"}, 0, 4, true); err != nil {
			t.Fatal(err)
		}
		events, _ := r.a.store.Read(ctx, sid, 0)
		var found bool
		for _, msg := range reconstruct(events) {
			for _, part := range msg.Parts {
				if res := part.ToolResult; res != nil && res.CallID == "c_bash" && strings.Contains(string(res.Content), "[inventory routine]") {
					found = true
					if !strings.Contains(string(res.Content), "bundle twelve") || !strings.Contains(string(res.Content), "bundle batchD") {
						t.Fatalf("wrong held-out values: %s", res.Content)
					}
				}
			}
		}
		if found != (i == 3) {
			t.Fatalf("turn %d automatic=%v, log=%s", i, found, r.logs.text())
		}
	}
}

func TestInventoryEdgeRequiresCompleteUniqueBindings(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell")
	}
	r := newRoutineRig(t, t.TempDir(), routineFile)
	feed := func(items []string, consumer string) string {
		raw, _ := json.Marshal(map[string]any{"rows": items, "field": "constant"})
		inventory := resultInventory("custom-index", json.RawMessage(`{"view":"open"}`), session.ToolResult{Content: raw})
		if consumer != "" {
			cmd, _ := json.Marshal(map[string]string{"command": consumer})
			r.host.InventoryRoutine(r.s.Workdir, inventory, string(cmd))
		}
		return r.host.InventoryRoutine(r.s.Workdir, inventory, "")
	}
	for _, tc := range []struct {
		ids []string
		cmd string
	}{
		{[]string{"a", "b"}, "processor -- a"},
		{[]string{"c", "d", "e"}, "processor -- e d c"},
		{[]string{"f", "g"}, "processor --select unrelated -- f g"},
		{[]string{"h", "i"}, "processor --select constant -- h i"},
		{[]string{"j", "k", "l"}, "processor --select constant -- j k l"},
	} {
		if got := feed(tc.ids, tc.cmd); got != "" {
			t.Fatalf("unverified binding: %s", got)
		}
	}
	// A different observed complete consumer makes the producer ambiguous.
	if got := feed([]string{"m", "n"}, "another -- m n"); got != "" {
		t.Fatal(got)
	}
	if got := feed([]string{"p", "q", "r"}, "another -- p q r"); got != "" {
		t.Fatal(got)
	}
	for _, body := range []string{`["a","a"]`, `{"rows":["a"],"more":["b"]}`, `["a",null]`, `[]`} {
		if resultInventory("custom-index", nil, session.ToolResult{Content: json.RawMessage(body)}) != "" {
			t.Fatalf("invalid inventory: %s", body)
		}
	}
}

func TestRoutineTraceSeparatesPipeStagesFromTheirBashCall(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	inventory := `{"items":["private-a","private-b","private-c"]}`
	for i := range 2 {
		r.fire("user_message", "text", "inspect selected paths")
		r.fire("tool_finished", "tool", "bash", "args", `{"command":"echo heading; collect -- private-a private-c | retain-lines | summarize"}`, "ok", "true", "call_id", fmt.Sprint(i), "order_known", "true", "inventory_items", inventoryItemDigests(r.s.Workdir, inventory))
		r.fire("turn_finished")
	}
	if err := r.command(t, "routine-trace", "2"); err != nil {
		t.Fatal(err)
	}
	log := r.logs.text()
	for _, s := range []string{"#1 statement 2: collect [2/3 inventory items] | retain-lines | summarize", "#2 statement 2: collect [2/3 inventory items] | retain-lines | summarize"} {
		if !strings.Contains(log, s) {
			t.Fatalf("missing %q: %s", s, log)
		}
	}
	if strings.Contains(log, "private-a") {
		t.Fatal("trace copied operand")
	}
	files, _ := filepath.Glob(filepath.Join(r.s.Workdir, ".magi/routines/*/routine.md"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "description: Writing sequence: bash") {
			t.Fatal("pipeline also learned as an opaque Bash wrapper")
		}
	}
}

func TestDependencyGraphRecallsSharedInputsAcrossDifferentPipelineTails(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	const prompt = "inspect selected artifacts"
	observe := func(item, later, tail string) {
		t.Helper()
		inventory := fmt.Sprintf(`{"items":[%q,"unused"]}`, item)
		id := inventoryIdentity(r.s.Workdir, inventory)
		r.fire("user_message", "text", prompt)
		r.fire("tool_finished", "tool", "list", "args", "{}", "ok", "true", "order_known", "true", "inventory_produced", id)
		for range 15 {
			r.fire("tool_finished", "tool", "read", "args", `{"path":"elsewhere"}`, "ok", "true", "order_known", "true")
		}
		args, _ := json.Marshal(map[string]string{"command": "collect -- " + item + " | reduce -- " + later + " | " + tail})
		r.fire("tool_finished", "tool", "bash", "args", string(args), "ok", "true", "order_known", "true",
			"inventory_read", id, "inventory_items", inventoryItemDigests(r.s.Workdir, inventory))
	}
	hint := func(prompt string) string {
		t.Helper()
		h, err := r.a.contextProviders[0].Provide(context.Background(), port.ContextQuery{SessionID: r.sid, Workdir: r.s.Workdir, Prompt: prompt})
		if err != nil {
			t.Fatal(err)
		}
		if len(h) == 0 {
			return ""
		}
		return h[0].Text
	}
	observe("private-a", "private-a", "summarize")
	if got := hint(prompt); got != "" {
		t.Fatalf("one observation: %s", got)
	}
	observe("private-b", "different", "finish")
	if got := hint(prompt); !strings.Contains(got, "list → collect") || strings.Contains(got, "reduce") {
		t.Fatalf("distinct operand binding: %s", got)
	}
	observe("private-c", "private-c", "archive")
	got := hint(prompt)
	if !strings.Contains(got, "list → collect → reduce") || strings.Contains(got, "archive") || strings.Contains(got, "private-") || strings.Contains(got, "elsewhere") {
		t.Fatalf("expected shared dependency slice: %s", got)
	}
	if got := hint("unrelated request"); got != "" {
		t.Fatalf("unrelated recall: %s", got)
	}
}
