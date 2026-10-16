package lua

import (
	"os"
	"path/filepath"
	"testing"
)

// Opt-in only: four short real calls verify the installed CLI protocol. Normal
// tests use the deterministic peer and never inherit a user's login.
func TestCodexAppServerRealTransport(t *testing.T) {
	if os.Getenv("MAGI_CODEX_REAL_TRANSPORT") != "1" {
		t.Skip("opt-in real Codex calls")
	}
	transport, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "tools", "codex-app-server-transport.lua"))
	if err != nil {
		t.Fatal(err)
	}
	code := `
local mcp = nil
local function jstr(v) return magi.json_encode(v) end
local function readLine(_)
 for i=1,3 do
  local line,err=mcp.ch:read({timeout='60s'})
  if line then return line end
  if err then return nil,err end
 end
 return nil,'real test timeout'
end
local function liveDrop(why)
 if mcp then mcp.ch:close() end
 mcp=nil
 magi.log('drop:'..tostring(why))
end
` + string(transport) + `
assert(liveOpen(), 'real initialize failed')
local a,e=liveCall('codex','{"prompt":"Reply with exactly OK. Do not use tools.","model":"gpt-6.1-sol"}',1)
assert(a,e)
magi.log('first thread='..a.thread..' usage='..magi.json_encode(a.usage)..' reply='..a.text)
local b; b,e=liveCall('codex-reply','{"threadId":'..jstr(a.thread)..',"prompt":"Reply with exactly NEXT. Do not use tools."}',1)
assert(b,e)
assert(a.thread==b.thread, 'thread changed')
assert(a.usage and b.usage,'missing usage')
magi.log('second same-thread usage='..magi.json_encode(b.usage)..' reply='..b.text)
for i=3,4 do
 local c; c,e=liveCall('codex-reply','{"threadId":'..jstr(a.thread)..',"prompt":"Reply with exactly OK. Do not use tools."}',1)
 assert(c,e)
 assert(c.thread==a.thread and c.usage,'continuity or usage missing')
 magi.log('turn '..i..' same-thread usage='..magi.json_encode(c.usage))
end
liveDrop('real test finished')
`
	h, out := loadHost(t, HostConfig{}, "name=\"codex-real-transport-test\"\nexec_timeout=\"5m\"\npermissions=[\"exec:codex\"]", code)
	defer h.Unload("codex-real-transport-test")
	t.Log(out)
}
