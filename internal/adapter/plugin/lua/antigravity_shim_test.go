package lua

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	f1, first, last := ask(`{"model":"Gemini 3.8 Flash (High)","messages":[{"role":"system","content":"now it is early"},{"role":"user","content":"hi"}]}`)
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
	f2, _, _ := ask(`{"model":"Gemini 3.8 Flash (High)","messages":[{"role":"system","content":"now it is later"},{"role":"user","content":"hi"},{"role":"assistant","content":"hello turn1"},{"role":"user","content":"again"}]}`)
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
}
