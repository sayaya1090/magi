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
	stat := observedReviewStat(rawArgs, session.ToolResult{Content: rawText})
	var invocation struct {
		Name string
		Args map[string]any
	}
	if plan := r.host.ReviewRoutine(r.s.Workdir, task, stat); json.Unmarshal([]byte(plan), &invocation) != nil {
		t.Fatalf("missing plan: %q", plan)
	}
	name := invocation.Name
	if r.host.ReviewRoutine(r.s.Workdir, "list files", stat) != "" {
		t.Fatal("unexpected review")
	}
	inventoryOut, _, failed := r.callTask(t, "inventory", "bash", map[string]any{"command": inventoryCommand}, "allow", task)
	if failed || !strings.Contains(inventoryOut, "2 files changed") || !strings.Contains(inventoryOut, "[review routine]") || !strings.Contains(inventoryOut, "+keep second") {
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
