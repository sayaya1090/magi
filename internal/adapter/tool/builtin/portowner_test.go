package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// collectPortInodes is pure /proc/net/tcp parsing, so it is host-testable off Linux.
// Port 5328 == 0x14D0. The fixture mixes: a LISTEN row on the port, a server-side
// ESTABLISHED row on the SAME local port (must match), a CLIENT row that merely
// connected TO the port (its LOCAL port is ephemeral — must NOT match), a TIME_WAIT
// orphan with inode 0 (skipped), and a row on a different port (ignored).
func TestCollectPortInodes(t *testing.T) {
	const fixture = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:14D0 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 100200 1 0000000000000000 100 0 0 10 0
   1: 0100007F:14D0 0100007F:C001 01 00000000:00000000 00:00000000 00000000     0        0 100201 1 0000000000000000 20 0 0 10 0
   2: 0100007F:C000 0100007F:14D0 01 00000000:00000000 00:00000000 00000000     0        0 100300 1 0000000000000000 20 0 0 10 0
   3: 0100007F:14D0 00000000:0000 06 00000000:00000000 00:00000000 00000000     0        0 0 0 0000000000000000 0 0 0 10 0
   4: 00000000:14D1 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 100400 1 0000000000000000 100 0 0 10 0
`
	dir := t.TempDir()
	path := filepath.Join(dir, "tcp")
	if err := os.WriteFile(path, []byte(fixture), 0o644); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	collectPortInodes(path, 5328, out)

	want := map[string]string{"100200": "LISTEN", "100201": "ESTABLISHED"}
	if len(out) != len(want) {
		t.Fatalf("got %v, want %v", out, want)
	}
	for ino, st := range want {
		if out[ino] != st {
			t.Errorf("inode %s: got state %q, want %q", ino, out[ino], st)
		}
	}
	// The client-side socket (local ephemeral C000, connected to the port) must be absent.
	if _, ok := out["100300"]; ok {
		t.Error("client socket that connected TO the port must not be counted")
	}
	// The inode-0 TIME_WAIT orphan and the different-port row must be absent.
	if _, ok := out["100400"]; ok {
		t.Error("a different port's listener must not be counted")
	}
}

// A missing /proc file is a no-op, not a panic (off-Linux hosts, or /proc/net/tcp6 absent).
func TestCollectPortInodesMissingFile(t *testing.T) {
	out := map[string]string{}
	collectPortInodes(filepath.Join(t.TempDir(), "nope"), 80, out)
	if len(out) != 0 {
		t.Fatalf("missing file should yield nothing, got %v", out)
	}
}

// Execute rejects an out-of-range port before touching any platform code.
func TestPortOwnerExecuteValidation(t *testing.T) {
	for _, raw := range []string{`{"port":0}`, `{"port":70000}`, `{"port":-1}`} {
		res, _ := PortOwner{}.Execute(context.Background(), json.RawMessage(raw), port.ToolEnv{})
		if !res.IsError {
			t.Errorf("%s: expected an error result", raw)
		}
		if !strings.Contains(string(res.Content), "1..65535") {
			t.Errorf("%s: expected a range message, got %s", raw, res.Content)
		}
	}
}

// Off Linux the tool reports itself unsupported rather than silently claiming the
// port is free (there is no /proc/net/tcp to consult).
func TestPortOwnerUnsupportedOffLinux(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("supported on Linux — covered by the integration test")
	}
	res, _ := PortOwner{}.Execute(context.Background(), json.RawMessage(`{"port":8080}`), port.ToolEnv{})
	if !res.IsError || !strings.Contains(string(res.Content), "Linux") {
		t.Fatalf("off Linux must report unsupported, got IsError=%v %s", res.IsError, res.Content)
	}
}
