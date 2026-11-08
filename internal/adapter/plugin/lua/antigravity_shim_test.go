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
	"time"

	"github.com/sayaya1090/magi/internal/testenv"
)

// The antigravity shim, end to end, against a fake `agy` that speaks the stream-json dialect
// measured on 2026-09-05. Three things it must do that the `agy --print`-per-request shim did not:
//  1. stream — the first content frame arrives before the child's last line;
//  2. keep one child per conversation — a request whose messages extend the last one is a second
//     turn of the SAME child (the fake answers "turnN"), not a fresh process;
//  3. keep TOOL_CALL lines out of the prose and hand them back as tool_calls.
//
// Thinking arrives as reasoning_content, and usage (with cache reads) closes the stream.
func TestAntigravityShimStreamsAndKeepsOneChildPerConversation(t *testing.T) {
	testenv.NeedSymlink(t)
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "plugins", "antigravity"))
	if err != nil {
		t.Fatal(err)
	}
	// The plugin lives outside the repository on purpose (.git/info/exclude) — where it is absent
	// this test has nothing to measure and says so rather than failing.
	manifest, err := os.ReadFile(filepath.Join(root, "plugin.toml"))
	if err != nil {
		t.Skipf("plugins/antigravity is not checked out here: %v", err)
	}
	initLua, err := os.ReadFile(filepath.Join(root, "init.lua"))
	if err != nil {
		t.Fatal(err)
	}
	fake, _ := filepath.Abs(filepath.Join("testdata", "fake-agy.sh"))
	bin := t.TempDir()
	if err := os.Symlink(fake, filepath.Join(bin, "agy")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	reg := &stubBaseReg{}
	h, out := loadHost(t, HostConfig{BaseReg: reg, PluginConfigs: map[string]map[string]any{
		"antigravity": {"defer_to_claude": false, "model": "Gemini 3.8 Flash (High)"},
	}}, string(manifest), string(initLua))
	defer func() { _ = h.Unload("antigravity") }()
	var port int
	for _, line := range strings.Split(out, "\n") {
		if i := strings.Index(line, "shim on 127.0.0.1:"); i >= 0 {
			fmt.Sscanf(line[i:], "shim on 127.0.0.1:%d", &port)
		}
	}
	if port == 0 {
		t.Fatalf("the shim did not start; logs:\n%s", out)
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/v1/chat/completions", port)

	ask := func(body string) (frames []string, firstAt, lastAt time.Duration) {
		t.Helper()
		start := time.Now()
		resp, err := http.Post(url, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("status %d", resp.StatusCode)
		}
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			if firstAt == 0 {
				firstAt = time.Since(start)
			}
			frames = append(frames, strings.TrimPrefix(line, "data: "))
		}
		lastAt = time.Since(start)
		return
	}
	joined := func(frames []string) string { return strings.Join(frames, "\n") }

	// 1. first turn: streams, thinks, closes with usage.
	// Every request ends with magi's one-request user message ("# Runtime context …") that the next
	// request no longer has — it must not count as part of the conversation's identity.
	f1, first, last := ask(`{"model":"Gemini 3.8 Flash (High)","messages":[{"role":"system","content":"now it is early"},{"role":"user","content":"hi"},{"role":"user","content":"# Runtime context (your live plan): step 1"}]}`)
	all := joined(f1)
	if !strings.Contains(all, `"content":"hello "`) || !strings.Contains(all, `"content":"turn1"`) {
		t.Fatalf("prose did not stream as content frames:\n%s", all)
	}
	if !strings.Contains(all, `"reasoning_content":"hmm"`) {
		t.Fatalf("thinking did not arrive as reasoning_content:\n%s", all)
	}
	if !strings.Contains(all, `"cached_tokens":4`) || !strings.Contains(all, `"finish_reason":"stop"`) {
		t.Fatalf("no usage / finish frame:\n%s", all)
	}
	if last-first < 200*time.Millisecond {
		t.Fatalf("the whole answer arrived at once (first %v, last %v) — not streamed", first, last)
	}

	// 2. second turn on the same conversation: the child remembers (turn2), no new process — even
	// though magi rebuilt the system message with a different context block (it does, every turn).
	f2, _, _ := ask(`{"model":"Gemini 3.8 Flash (High)","messages":[{"role":"system","content":"now it is later"},{"role":"user","content":"hi"},{"role":"assistant","content":"hello turn1 (as magi re-renders it)"},{"role":"user","content":"again"},{"role":"user","content":"# Runtime context (your live plan): step 2"}]}`)
	if !strings.Contains(joined(f2), `"content":"turn2"`) {
		t.Fatalf("second request did not continue the same child (expected turn2):\n%s", joined(f2))
	}

	// 3. a different conversation gets its own child (turn1 again), and a TOOL_CALL becomes tool_calls.
	f3, _, _ := ask(`{"model":"Gemini 3.8 Flash (High)","messages":[{"role":"user","content":"please CALLME"}],"tools":[{"type":"function","function":{"name":"list_slides","description":"x","parameters":{"type":"object"}}}]}`)
	all3 := joined(f3)
	if !strings.Contains(all3, `"name":"list_slides"`) || !strings.Contains(all3, `"finish_reason":"tool_calls"`) {
		t.Fatalf("TOOL_CALL did not become a tool_calls frame:\n%s", all3)
	}
	if strings.Contains(all3, `TOOL_CALL`) {
		t.Fatalf("the TOOL_CALL line leaked into the prose:\n%s", all3)
	}
	if !strings.Contains(all3, `"content":"Sure.\n"`) {
		t.Fatalf("the prose before the call was lost:\n%s", all3)
	}

	// 4. Council queries run (one-shot, no tools, judging instructions).
	// They must not evict conversation 3's child or be confused with it.
	for _, member := range []string{"Melchior", "Balthasar", "Casper"} {
		fc, _, _ := ask(fmt.Sprintf(`{"model":"Gemini 3.8 Flash (High)","messages":[{"role":"system","content":"You are %s, judging through the correctness lens."},{"role":"user","content":"check evidence"}]}`, member))
		if !strings.Contains(joined(fc), `"content":"turn1"`) {
			t.Fatalf("council call %s did not get its own child: %s", member, joined(fc))
		}
	}

	// 5. Turn 4 on conversation 3 continues the SAME child as f3 (turn2), NOT a new child!
	f4, _, _ := ask(`{"model":"Gemini 3.8 Flash (High)","messages":[{"role":"user","content":"please CALLME"},{"role":"assistant","content":"Sure.","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_slides","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"slides: [1, 2]"},{"role":"user","content":"next step"}],"tools":[{"type":"function","function":{"name":"list_slides","description":"x","parameters":{"type":"object"}}}]}`)
	all4 := joined(f4)
	if !strings.Contains(all4, `"content":"turn2"`) {
		t.Fatalf("turn 4 on conversation 3 did not continue the same child (expected turn2 after council queries):\n%s", all4)
	}

	// 6. Intermediate turns handled by another provider (assistant prose + tool_calls) are fed in
	// renderTail when switching back to agy on the same child (caught_up_turn3).
	f5, _, _ := ask(`{"model":"Gemini 3.8 Flash (High)","messages":[{"role":"user","content":"please CALLME"},{"role":"assistant","content":"Sure.","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_slides","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"slides: [1, 2]"},{"role":"user","content":"next step"},{"role":"assistant","content":"hello turn2"},{"role":"user","content":"switched away"},{"role":"assistant","content":"OTHER_PROVIDER_ANSWER","tool_calls":[{"id":"call_other","type":"function","function":{"name":"list_slides","arguments":"{\"filter\":\"OTHER_PROVIDER_ARG\"}"}}]},{"role":"tool","tool_call_id":"call_other","content":"slides: [3]"},{"role":"user","content":"switched back to agy"}],"tools":[{"type":"function","function":{"name":"list_slides","description":"x","parameters":{"type":"object"}}}]}`)
	all5 := joined(f5)
	if !strings.Contains(all5, `"content":"caught_up_turn3"`) {
		t.Fatalf("intermediate assistant turns from another provider were lost in renderTail:\n%s", all5)
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
	pResp, err := http.Post(prefillURL, "application/json", strings.NewReader(`{"model":"Gemini 3.8 Flash (High)","messages":[{"role":"user","content":"# Chronological turn history\n## 1 Tool result\nfile created"}]}`))
	if err != nil {
		t.Fatalf("POST /v1/prefill: %v", err)
	}
	defer pResp.Body.Close()
	if pResp.StatusCode != 200 {
		t.Fatalf("POST /v1/prefill status: %d", pResp.StatusCode)
	}
}
