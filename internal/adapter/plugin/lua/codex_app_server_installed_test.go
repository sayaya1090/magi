package lua

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The optional plugin lives outside Git. This opt-in check exercises its full
// migrated HTTP handler, while the unconditional protocol test covers the shipped transport.
func TestCodexAppServerPatchedPlugin(t *testing.T) {
	path := os.Getenv("MAGI_CODEX_PLUGIN_UNDER_TEST")
	if path == "" {
		p, _ := filepath.Abs(filepath.Join("..", "..", "..", "..", "plugins", "codex", "init.lua"))
		if _, err := os.Stat(p); err == nil {
			path = p
		} else {
			t.Skip("optional local plugin path not supplied")
		}
	}
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 unavailable")
	}
	code, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(filepath.Dir(path), "plugin.toml"))
	if err != nil {
		t.Fatal(err)
	}
	fake, err := os.ReadFile(filepath.Join("testdata", "fake-codex-app-server.py"))
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "codex"), fake, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	argvLog := filepath.Join(bin, "argv.log")
	t.Setenv("FAKE_CODEX_ARGV_LOG", argvLog)
	h, out := loadHost(t, HostConfig{BaseReg: &stubBaseReg{}, PluginConfigs: map[string]map[string]any{"codex": {"defer_to_claude": false, "model": "test"}}}, string(manifest), string(code))
	defer h.Unload("codex")
	port := 0
	for _, line := range strings.Split(out, "\n") {
		if i := strings.Index(line, "shim on 127.0.0.1:"); i >= 0 {
			fmt.Sscanf(line[i:], "shim on 127.0.0.1:%d", &port)
		}
	}
	if port == 0 {
		t.Fatal(out)
	}
	ask := func(body string) (int, string) {
		t.Helper()
		resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/v1/chat/completions", port), "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, string(b)
	}
	status, body := ask(`{"model":"test","messages":[{"role":"user","content":"hi"}]}`)
	if status != 200 || !strings.Contains(body, "thread-1/turn-1") {
		t.Fatalf("%d %s", status, body)
	}
	// codex's own retries are the harness's to own: the shim asks the app-server which provider it
	// routes through (config/read) and starts it again with that provider's retries off.
	rawArgv, err := os.ReadFile(argvLog)
	if err != nil {
		t.Fatal(err)
	}
	starts := strings.Split(strings.TrimSpace(string(rawArgv)), "\n")
	last := starts[len(starts)-1]
	if len(starts) != 2 || !strings.Contains(last, `"model_providers.fakep.request_max_retries=0"`) ||
		!strings.Contains(last, `"model_providers.fakep.stream_max_retries=0"`) {
		t.Fatalf("the app-server should be restarted once with the provider's CLI retries off; starts:\n%s", rawArgv)
	}
	status, body = ask(`{"model":"test","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"first answer"},{"role":"user","content":"again"}]}`)
	if status != 200 || !strings.Contains(body, "thread-1/turn-2") || !strings.Contains(body, `"cached_tokens":900`) {
		t.Fatalf("%d %s", status, body)
	}
	status, body = ask(`{"model":"test","messages":[{"role":"user","content":"FAIL"}]}`)
	if status != 409 || !strings.Contains(body, "자동 재요청하지 않았습니다") {
		t.Fatalf("submitted failure must not replay through exec or a retryable HTTP status: %d %s", status, body)
	}

	// 4. /props advertises prefill capability
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

	// 5. /v1/prefill pre-warms background session
	prefillURL := fmt.Sprintf("http://127.0.0.1:%d/v1/prefill", port)
	pResp, err := http.Post(prefillURL, "application/json", strings.NewReader(`{"model":"test","messages":[{"role":"user","content":"prefill message"}]}`))
	if err != nil {
		t.Fatalf("POST /v1/prefill: %v", err)
	}
	defer pResp.Body.Close()
	if pResp.StatusCode != 200 {
		t.Fatalf("POST /v1/prefill status: %d", pResp.StatusCode)
	}
}
