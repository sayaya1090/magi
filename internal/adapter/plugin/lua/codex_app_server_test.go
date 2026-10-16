package lua

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Exercises the shipped transport itself, independently of a user's optional
// Codex plugin and without spending API tokens or inheriting their credentials.
func TestCodexAppServerReusesThreadAndAccountsUsageOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable script fixture uses POSIX shebang")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 unavailable")
	}
	transport, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "tools", "codex-app-server-transport.lua"))
	if err != nil {
		t.Fatal(err)
	}
	fake, err := filepath.Abs(filepath.Join("testdata", "fake-codex-app-server.py"))
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	body, err := os.ReadFile(fake)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "codex"), body, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	prelude := `
local mcp = nil
local function jstr(v) return magi.json_encode(v) end
local function readLine(_) return mcp.ch:read({timeout='5s'}) end
local function liveDrop(why)
  if mcp then mcp.ch:close() end
  mcp = nil
  magi.log('drop:' .. tostring(why))
end
`
	checks := `
assert(liveOpen(), 'initialize failed')
local a, e = liveCall('codex', '{"prompt":"first","model":"test"}', 1)
assert(a, e)
assert(a.thread == 'thread-1' and a.text == 'thread-1/turn-1:first', a.text)
assert(a.usage.prompt == 1000 and a.usage.cached == 0 and a.usage.completion == 10)
local b; b, e = liveCall('codex-reply', '{"threadId":"thread-1","prompt":"second"}', 1)
assert(b, e)
assert(b.thread == a.thread and b.text == 'thread-1/turn-2:second', b.text)
assert(b.usage.prompt == 1000 and b.usage.cached == 900 and b.usage.completion == 10)
local c; c, e = liveCall('codex', '{"prompt":"independent"}', 1)
assert(c, e)
assert(c.thread == 'thread-2' and c.usage.cached == 0)
local d; d, e = liveCall('codex-reply', '{"threadId":"thread-2","prompt":"FAIL"}', 1)
assert(d == nil and e == 'Codex turn failed', tostring(e))
liveDrop('test finished')
magi.log('app-server checks passed')
`
	h, out := loadHost(t, HostConfig{}, "name=\"codex-transport-test\"\npermissions=[\"exec:codex\"]", prelude+string(transport)+checks)
	defer h.Unload("codex-transport-test")
	if !strings.Contains(out, "app-server checks passed") {
		t.Fatal(out)
	}
}
