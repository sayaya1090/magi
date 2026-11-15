package lua

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/testenv"
)

func TestClaudeCodeShimSessionReuseContextCapAndSafetyClassifier(t *testing.T) {
	testenv.NeedSymlink(t)
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "plugins", "claudecode"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(root, "plugin.toml"))
	if err != nil {
		t.Skipf("plugins/claudecode is not checked out here: %v", err)
	}
	initLua, err := os.ReadFile(filepath.Join(root, "init.lua"))
	if err != nil {
		t.Fatal(err)
	}
	fake, _ := filepath.Abs(filepath.Join("testdata", "fake-claude.sh"))
	if err := os.Chmod(fake, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.Symlink(fake, filepath.Join(bin, "claude")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	reg := &stubBaseReg{}
	h, out := loadHost(t, HostConfig{BaseReg: reg, PluginConfigs: map[string]map[string]any{
		"claudecode": {"model": "opus", "context_window": 200000},
	}}, string(manifest), string(initLua))
	defer func() { _ = h.Unload("claudecode") }()
	var port int
	for _, line := range strings.Split(out, "\n") {
		if i := strings.Index(line, "shim on 127.0.0.1:"); i >= 0 {
			fmt.Sscanf(line[i:], "shim on 127.0.0.1:%d", &port)
		}
	}
	if port == 0 {
		t.Fatalf("the claudecode shim did not start; logs:\n%s", out)
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/v1/chat/completions", port)
	modelsURL := fmt.Sprintf("http://127.0.0.1:%d/v1/models", port)

	ask := func(body string) string {
		t.Helper()
		resp, err := http.Post(url, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("status %d", resp.StatusCode)
		}
		var frames []string
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "data: ") {
				frames = append(frames, strings.TrimPrefix(line, "data: "))
			}
		}
		return strings.Join(frames, "\n")
	}

	toolsJSON := `"tools":[{"type":"function","function":{"name":"list_slides","description":"x","parameters":{"type":"object"}}}]`

	// 1. First turn with tool call.
	f1 := ask(`{"model":"opus","messages":[{"role":"system","content":"turn 1 sys"},{"role":"user","content":"please CALLME"},{"role":"user","content":"# Runtime context: step 1"}],` + toolsJSON + `}`)
	if !strings.Contains(f1, `"name":"list_slides"`) {
		t.Fatalf("expected tool_call on turn 1:\n%s", f1)
	}

	// 1a. A call written after prose on the same line, split across stream deltas mid-marker, must
	// run as a call and its text must not reach the reply (users saw TOOL_CALL text in the bubble).
	fm := ask(`{"model":"opus","messages":[{"role":"user","content":"please MIDLINE_CALL"}],` + toolsJSON + `}`)
	if !strings.Contains(fm, `"name":"list_slides"`) || !strings.Contains(fm, "Let me look.") {
		t.Fatalf("a call after prose on one line did not become a tool call:\n%s", fm)
	}
	if strings.Contains(fm, "TOOL_C") || strings.Contains(fm, "ALL {") {
		t.Fatalf("TOOL_CALL text leaked into the reply:\n%s", fm)
	}

	// 1b. The model sometimes calls a tool in its own <invoke> form instead of a TOOL_CALL line
	// (Terminal-Bench circuit-fibsqrt: the turn ended in 3s with the block as prose). It must run as a
	// tool call and its text must not reach the reply.
	fi := ask(`{"model":"opus","messages":[{"role":"user","content":"please NATIVE_INVOKE"}],` + toolsJSON + `}`)
	if !strings.Contains(fi, `"name":"list_slides"`) || !strings.Contains(fi, `\"deck\":\"main\"`) || !strings.Contains(fi, `\"limit\":3`) {
		t.Fatalf("a native <invoke> block did not become a tool call:\n%s", fi)
	}
	if strings.Contains(fi, "invoke") || strings.Contains(fi, "parameter name") {
		t.Fatalf("<invoke> text leaked into the reply:\n%s", fi)
	}

	// 1c. A malformed call is not shown with a "NOT run" note: the same child is asked once for a
	// corrected call, and nothing repeats. The prose before it goes out once, the retry's own
	// prose and thinking never reach the reply, and exactly one corrected call is run.
	fb := ask(`{"model":"opus","messages":[{"role":"user","content":"please BROKEN_CALL"}],` + toolsJSON + `}`)
	if strings.Count(fb, `"name":"list_slides"`) != 1 || !strings.Contains(fb, `\"deck\":\"main\"`) {
		t.Fatalf("a malformed call was not replaced by exactly one corrected call:\n%s", fb)
	}
	if strings.Count(fb, "Checking the deck.") != 1 {
		t.Fatalf("the prose before a malformed call must go out exactly once:\n%s", fb)
	}
	for _, leak := range []string{"NOT run", "Sorry, here it is", "retry thinking", `main}}`} {
		if strings.Contains(fb, leak) {
			t.Fatalf("%q reached the reply:\n%s", leak, fb)
		}
	}
	// A mention that only looks like a call is kept as written, once, when the model says so.
	fp := ask(`{"model":"opus","messages":[{"role":"user","content":"please MENTION_CALL"}],` + toolsJSON + `}`)
	if strings.Count(fp, "per line.") != 1 || !strings.Contains(fp, "The format is") || !strings.Contains(fp, "TOOL_CALL {...}") || strings.Contains(fp, "NOT_A_CALL") || strings.Contains(fp, "NOT run") || strings.Contains(fp, `"tool_calls"`) {
		t.Fatalf("a mention of the call format must stay prose, shown once:\n%s", fp)
	}
	// Malformed twice: one short notice, no raw call text, nothing run.
	ft := ask(`{"model":"opus","messages":[{"role":"user","content":"please BROKEN_TWICE"}],` + toolsJSON + `}`)
	if strings.Count(ft, "was malformed and was not run") != 1 || strings.Contains(ft, `main}}`) || strings.Contains(ft, `"tool_calls"`) || strings.Contains(ft, "NOT run") {
		t.Fatalf("a call malformed twice must leave one notice and nothing run:\n%s", ft)
	}

	// 2. Context window cap: fake-claude reported 1,000,000 in modelUsage, but /v1/models must cap at cfg_context (200,000).
	mresp, err := http.Get(modelsURL)
	if err != nil {
		t.Fatalf("GET /models: %v", err)
	}
	mbody, _ := io.ReadAll(mresp.Body)
	mresp.Body.Close()
	if strings.Contains(string(mbody), "1000000") || !strings.Contains(string(mbody), `"context_length":200000`) {
		t.Fatalf("expected context_length to be capped at 200000, got:\n%s", string(mbody))
	}

	// 3. Council one-shot queries (4 calls, exceeding MAX_SESSIONS=4 if not closed/prioritized) must not evict the agent child.
	for _, member := range []string{"Melchior", "Balthasar", "Casper", "Rebuttal"} {
		fc := ask(fmt.Sprintf(`{"model":"opus","messages":[{"role":"system","content":"You are %s, judging through the correctness lens."},{"role":"user","content":"check evidence"}]}`, member))
		if !strings.Contains(fc, `"hello turn1\n"`) {
			t.Fatalf("council call %s did not run on its own child:\n%s", member, fc)
		}
	}

	// 4. Turn 2 on the agent conversation: even with a changed system prompt, elided tool output on later turns,
	// and after 4 council calls, it continues the SAME child (turn2).
	f2 := ask(`{"model":"opus","messages":[{"role":"system","content":"turn 2 sys changed"},{"role":"user","content":"please CALLME"},{"role":"assistant","content":"Sure. (re-rendered)","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_slides","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"slides: [1, 2]"},{"role":"user","content":"next step"},{"role":"user","content":"# Runtime context: step 2"}],` + toolsJSON + `}`)
	if !strings.Contains(f2, `"hello turn2\n"`) {
		t.Fatalf("turn 2 did not continue the same child (expected turn2):\n%s", f2)
	}

	// 5. Turn 3 after eliding call_1's tool output and switching to another provider in between: still continues the same child (caught_up_turn3).
	f3 := ask(`{"model":"opus","messages":[{"role":"system","content":"turn 3 sys"},{"role":"user","content":"please CALLME"},{"role":"assistant","content":"Sure.","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_slides","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"[elided tool output]"},{"role":"user","content":"next step"},{"role":"assistant","content":"hello turn2"},{"role":"user","content":"switched away"},{"role":"assistant","content":"OTHER_PROVIDER_ANSWER","tool_calls":[{"id":"call_other","type":"function","function":{"name":"list_slides","arguments":"{\"filter\":\"OTHER_PROVIDER_ARG\"}"}}]},{"role":"tool","tool_call_id":"call_other","content":"slides: [3]"},{"role":"user","content":"switched back"}],` + toolsJSON + `}`)
	if !strings.Contains(f3, `"caught_up_turn3\n"`) {
		t.Fatalf("turn 3 did not preserve intermediate provider turn on same child:\n%s", f3)
	}

	// 6. Safety classifier interruption: discards partial TOOL_CALL from aborted stream and surfaces the safety marker.
	f4 := ask(`{"model":"opus","messages":[{"role":"user","content":"TRIGGER_SAFETY"}]}`)
	if !strings.Contains(f4, `[claudecode: response stopped by backend safety classifier]`) || !strings.Contains(f4, `fallback_turn1`) {
		t.Fatalf("expected safety classifier marker and fallback text:\n%s", f4)
	}
	if strings.Contains(f4, `"tool_calls"`) {
		t.Fatalf("aborted partial TOOL_CALL before safety classifier should have been discarded:\n%s", f4)
	}

	// 7. /props advertises prefill capability for council real-time accumulation.
	propsURL := fmt.Sprintf("http://127.0.0.1:%d/props", port)
	presp, err := http.Get(propsURL)
	if err != nil {
		t.Fatalf("GET /props: %v", err)
	}
	pbody, _ := io.ReadAll(presp.Body)
	presp.Body.Close()
	if !strings.Contains(string(pbody), `"prefill_endpoint":"/v1/prefill"`) {
		t.Fatalf("expected prefill_endpoint in /props, got:\n%s", string(pbody))
	}

	// 8. /v1/prefill pre-warms background council session.
	prefillURL := fmt.Sprintf("http://127.0.0.1:%d/v1/prefill", port)
	pResp, err := http.Post(prefillURL, "application/json", strings.NewReader(`{"model":"opus","messages":[{"role":"user","content":"# Chronological turn history\n## 1 Tool result\nfile created"}]}`))
	if err != nil {
		t.Fatalf("POST /v1/prefill: %v", err)
	}
	defer pResp.Body.Close()
	if pResp.StatusCode != 200 {
		t.Fatalf("POST /v1/prefill status: %d", pResp.StatusCode)
	}
}
