package lua

import (
	"context"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

// loadPiped is loadOut plus a handle on the host, so a test can unload the plugin afterwards and
// assert what happened to the child it left running.
func loadPiped(t *testing.T, manifest, initLua string) (*Host, string, error) {
	t.Helper()
	var logged strings.Builder
	h := NewHostWithConfig(HostConfig{
		Runtime: RuntimeInfo{Workdir: t.TempDir()},
		Logf:    func(s string) { logged.WriteString(s + "\n") },
	})
	dir := writePlugin(t, manifest, initLua)
	_, err := h.Load(context.Background(), dir)
	return h, logged.String(), err
}

// A live child is gated on the SAME exec:<cmd> permission a one-shot run is: the reach (which
// binary, which arguments) is identical, so a second permission name for it would be two words
// for one capability.
func TestPipeDeniedWithoutExecGrant(t *testing.T) {
	out, err := loadOut(t,
		`name="x"`+"\n"+`capabilities=["tool"]`,
		`local ch, e = magi.pipe("cat")
magi.log("denied=" .. tostring(ch == nil) .. " err=" .. tostring(e))`,
	)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(out, "denied=true") || !strings.Contains(out, "permission denied: exec:cat") {
		t.Errorf("pipe should be denied without exec:cat, got: %q", out)
	}
}

// The point of the whole bridge: the child is still there on the second exchange. Two writes and
// two reads on one handle, and the answers come back in order from one process.
func TestPipeKeepsOneChildAcrossExchanges(t *testing.T) {
	out, err := loadOut(t,
		`name="x"`+"\n"+`permissions=["exec:cat"]`,
		`local ch = magi.pipe("cat")
ch:write("first")
ch:write("second")
local a = ch:read({timeout="10s"})
local b = ch:read({timeout="10s"})
magi.log("a=" .. tostring(a) .. " b=" .. tostring(b) .. " alive=" .. tostring(ch:alive()))
ch:close()`,
	)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(out, "a=first b=second") {
		t.Errorf("both lines should come back in order from one child, got: %q", out)
	}
	if !strings.Contains(out, "alive=true") {
		t.Errorf("the child should still be alive between exchanges, got: %q", out)
	}
}

// A missing trailing newline is added rather than left to hang. On a line protocol a write with
// no newline arrives nowhere and the matching read waits out its whole deadline — a silent stall
// with nothing in any log to explain it.
func TestPipeAddsTheMissingNewline(t *testing.T) {
	out, err := loadOut(t,
		`name="x"`+"\n"+`permissions=["exec:cat"]`,
		`local ch = magi.pipe("cat")
ch:write("no-newline-here")
magi.log("got=" .. tostring(ch:read({timeout="10s"})))
ch:close()`,
	)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(out, "got=no-newline-here") {
		t.Errorf("a write without a newline should still deliver a line, got: %q", out)
	}
}

// A deadline with nothing to read is nil AND no error — "nothing yet", which the caller can tell
// apart from a dead child and decide to wait again on. Returning an error here would make every
// quiet moment look like a failure.
func TestPipeReadTimeoutIsNotAnError(t *testing.T) {
	out, err := loadOut(t,
		`name="x"`+"\n"+`permissions=["exec:cat"]`,
		`local ch = magi.pipe("cat")
local line, e = ch:read({timeout="150ms"})
magi.log("line=" .. tostring(line) .. " err=" .. tostring(e) .. " alive=" .. tostring(ch:alive()))
ch:close()`,
	)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(out, "line=nil err=nil") {
		t.Errorf("a timeout should be nil with no error, got: %q", out)
	}
	if !strings.Contains(out, "alive=true") {
		t.Errorf("a quiet child is not a dead one, got: %q", out)
	}
}

// Reading a child that has been closed reports the death instead of waiting out the deadline.
func TestPipeReadAfterCloseReportsDeath(t *testing.T) {
	out, err := loadOut(t,
		`name="x"`+"\n"+`permissions=["exec:cat"]`,
		`local ch = magi.pipe("cat")
ch:close()
local line, e = ch:read({timeout="5s"})
magi.log("line=" .. tostring(line) .. " err=" .. tostring(e) .. " alive=" .. tostring(ch:alive()))`,
	)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(out, "line=nil") || !strings.Contains(out, "child exited") {
		t.Errorf("reading a closed child should report the exit, got: %q", out)
	}
	if !strings.Contains(out, "alive=false") {
		t.Errorf("a closed child is not alive, got: %q", out)
	}
}

// A plugin that forgets to close cannot fork-bomb the machine: past the cap, pipe refuses and
// says so, rather than starting a fifth process nobody is tracking.
func TestPipeCapsLiveChildren(t *testing.T) {
	out, err := loadOut(t,
		`name="x"`+"\n"+`permissions=["exec:cat"]`,
		`local kept = {}
local lastErr = ""
for i = 1, `+itoa(pipeMaxPerPlugin+1)+` do
  local ch, e = magi.pipe("cat")
  if ch then kept[#kept+1] = ch else lastErr = tostring(e) end
end
magi.log("opened=" .. tostring(#kept) .. " err=" .. lastErr)
for _, ch in ipairs(kept) do ch:close() end`,
	)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(out, "opened="+itoa(pipeMaxPerPlugin)) {
		t.Errorf("the cap should stop at %d children, got: %q", pipeMaxPerPlugin, out)
	}
	if !strings.Contains(out, "already alive") {
		t.Errorf("the refusal should say why, got: %q", out)
	}
}

// Unloading the plugin kills what it left running. A child that outlives its plugin is a process
// nobody owns and nobody will ever close.
func TestPipeChildDiesWithThePlugin(t *testing.T) {
	h, out, err := loadPiped(t,
		`name="pipey"`+"\n"+`permissions=["exec:cat"]`,
		`local ch = magi.pipe("cat")
magi.log("pid=" .. tostring(ch.pid))`,
	)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	pid := 0
	if i := strings.Index(out, "pid="); i >= 0 {
		for _, c := range out[i+4:] {
			if c < '0' || c > '9' {
				break
			}
			pid = pid*10 + int(c-'0')
		}
	}
	if pid == 0 {
		t.Fatalf("no pid in log: %q", out)
	}
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("child should be running before unload: %v", err)
	}
	if err := h.Unload("pipey"); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	// The kill is delivered synchronously; the reap is not, so give the OS a moment to make the
	// pid unreachable rather than asserting on the instant.
	gone := false
	for i := 0; i < 50; i++ {
		if err := syscall.Kill(pid, 0); err != nil {
			gone = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !gone {
		_ = syscall.Kill(pid, syscall.SIGKILL) // do not leave it behind either way
		t.Error("the child should not outlive its plugin")
	}
}

// A child that dies on its own reports its stderr, so "the backend is gone" comes with the reason
// the process printed on its way out instead of a bare failure.
func TestPipeReportsWhyTheChildDied(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh")
	}
	out, err := loadOut(t,
		`name="x"`+"\n"+`permissions=["exec:sh"]`,
		`local ch = magi.pipe("sh", {"-c", "echo boom-on-stderr 1>&2; exit 3"})
local line, e = ch:read({timeout="5s"})
magi.log("line=" .. tostring(line) .. " err=" .. tostring(e))`,
	)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(out, "boom-on-stderr") {
		t.Errorf("the error should carry the child's stderr, got: %q", out)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
