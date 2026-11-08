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
}
