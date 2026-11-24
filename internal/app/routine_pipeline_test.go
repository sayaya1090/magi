package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/platform"
	"github.com/sayaya1090/magi/internal/core/session"
)

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
	observe("first", "git show --format= $c -- a"+inc+"; git show --format= $c -- b c | grep -E '^[-+@]' | grep -v '^+++\\|^---'")
	observe("second", "git show --format= earlier -- a"+inc+tail)
	r.fire("user_message", "text", "review the other commits")
	observe("third", "git show --format= $c -- d e f")
	observe("fourth", "git show --format= $c -- g"+inc)
	events, _ := r.a.store.Read(context.Background(), r.sid, 0)
	body, _ := json.Marshal(reconstruct(events))
	names := regexp.MustCompile(`run_routine (write-[0-9a-f]+) accepts current inputs`).FindAllStringSubmatch(string(body), -1)
	if len(names) != 2 || names[0][1] != names[1][1] {
		t.Fatalf("expected one hint in each turn: %s", body)
	}
	name := names[0][1]
	// The two inventory calls have the same command structure. Only the first
	// output contains changed files. No git show pipeline has run in this turn.
	const inventory = `{"command":"git log --oneline old..HEAD; git show --stat --format= HEAD"}`
	postInventory := func(id, text string) {
		raw, _ := json.Marshal("exit 0\nHEAD abc1234 reviewed\norigin/main def1234 ahead 2\n" + text)
		stat := observedReviewStat(json.RawMessage(inventory), session.ToolResult{Content: raw})
		r.fire("tool_finished", "tool", "bash", "args", inventory, "ok", "true", "call_id", id, "review_stat", stat)
	}
	notes := func() []string {
		evs, _ := r.a.store.Read(context.Background(), r.sid, 0)
		var got []string
		for _, msg := range reconstruct(evs) {
			for _, part := range msg.Parts {
				if strings.HasPrefix(part.Text, "[routine] Changed files found.") {
					got = append(got, part.Text)
				}
			}
		}
		return got
	}
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
	r.fire("user_message", "text", "다른 세션의 커밋들 검토해 줘")
	postInventory("empty", "=== per-commit stat\n=== uncommitted tracked\n M misc")
	if len(notes()) != 0 {
		t.Fatal("inventory with no new commits triggered a hint")
	}
	statText := "=== per-commit stat\n--- " + revision + "\n changed one | 1 +\n changed two | 2 ++\n 2 files changed, 3 insertions(+)\n"
	postInventory("found", statText)
	postInventory("again", statText)
	got := notes()
	if len(got) != 1 {
		t.Fatalf("wanted exactly one actionable suggestion: %q", got)
	}
	var invocation struct {
		Name string
		Args map[string]any
	}
	_, payload, ok := strings.Cut(got[0], "call run_routine with ")
	if !ok || json.NewDecoder(strings.NewReader(payload)).Decode(&invocation) != nil || invocation.Name != name ||
		invocation.Args["s1_arg3"] != revision || fmt.Sprint(invocation.Args["items"]) != "[changed one changed two]" || invocation.Args["s2_arg2"] != "^[-+]" {
		t.Fatal(got[0])
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
