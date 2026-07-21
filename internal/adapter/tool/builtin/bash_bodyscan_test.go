package builtin

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// maskedFailureNote fires only when exit==0 AND the body carries a high-precision
// crash/traceback signature — the fingerprint of a `|| echo`/`|| true`-masked failure.
func TestMaskedFailureNote(t *testing.T) {
	goPanic := "some output\npanic: runtime error\n\ngoroutine 1 [running]:\nmain.main()"
	goFatal := "fatal error: concurrent map writes\n\ngoroutine 7 [running]:"
	for _, tc := range []struct {
		name string
		exit int
		body string
		want bool
	}{
		{"python traceback, exit 0", 0, "exit 0\nTraceback (most recent call last):\n  File x", true},
		{"jvm exception, exit 0", 0, "Exception in thread \"main\" java.lang.NullPointerException", true},
		{"go panic + goroutine, exit 0", 0, goPanic, true},
		{"go fatal + goroutine, exit 0", 0, goFatal, true},
		// Non-zero already surfaces as an error — the note would be redundant noise.
		{"python traceback, exit 1", 1, "Traceback (most recent call last):", false},
		// "panic:" as incidental data with no goroutine dump must not be flagged.
		{"bare panic word, exit 0", 0, "echo 'panic: do not panic'", false},
		{"fatal error compiler diag, exit 0", 0, "main.c:3:10: fatal error: stdio.h: No such file", false},
		{"clean output, exit 0", 0, "exit 0\nall tests passed\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := maskedFailureNote(tc.exit, tc.body) != ""
			if got != tc.want {
				t.Errorf("maskedFailureNote(exit=%d) = %v, want %v", tc.exit, got, tc.want)
			}
		})
	}
}

// The default is ON; only an explicit off-value disables the scan.
func TestBodyscanEnabledDefault(t *testing.T) {
	if !bodyscanEnabled() {
		t.Fatal("default must be ON")
	}
	for _, v := range []string{"0", "off", "false", "no", "OFF"} {
		t.Setenv("MAGI_EXITCODE_BODYSCAN", v)
		if bodyscanEnabled() {
			t.Errorf("%q must disable the scan", v)
		}
	}
	for _, v := range []string{"1", "on", "true", "yes", "whatever"} {
		t.Setenv("MAGI_EXITCODE_BODYSCAN", v)
		if !bodyscanEnabled() {
			t.Errorf("%q must leave the scan ON", v)
		}
	}
}

// The note carries the actionable hint (masked exit code) so the model and council both
// see why a clean-looking exit 0 is suspect.
func TestMaskedFailureNoteContent(t *testing.T) {
	note := maskedFailureNote(0, "Traceback (most recent call last):")
	if !strings.Contains(note, "exit 0") || !strings.Contains(note, "masked") {
		t.Errorf("note should explain the masked exit code, got %q", note)
	}
}

// maskingTailNote fires on exit 0 when the command's final list operator is a pure
// exit-code mask (true/:/exit 0/echo — things that can only hide a failure), and stays
// quiet for genuine fallback control flow (`cmd || other-cmd`).
func TestMaskingTailNote(t *testing.T) {
	for _, tc := range []struct {
		name string
		exit int
		cmd  string
		want bool
	}{
		{"|| true", 0, "python3 test.py || true", true},
		{"|| colon", 0, "make check || :", true},
		{"|| exit 0", 0, "./run-tests.sh || exit 0", true},
		{"|| echo with args", 0, `timeout 5 python3 t.py 2>&1 || echo "Exit code: $?"`, true},
		{"trailing whitespace", 0, "false || true  ", true},
		// Genuine fallbacks are intentional control flow, not masks.
		{"fallback command", 0, "which python3 || which python", false},
		{"fallback then real cmd after echo", 0, "x || echo retrying; make", false},
		{"no tail", 0, "python3 test.py", false},
		{"or inside, not at end", 0, "a || true && b", false},
		// Non-zero exit: the mask didn't engage (or didn't matter) — ✗ already speaks.
		{"non-zero exit", 3, "python3 test.py || true", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := maskingTailNote(tc.exit, tc.cmd) != ""
			if got != tc.want {
				t.Errorf("maskingTailNote(%d, %q) fired=%v, want %v", tc.exit, tc.cmd, got, tc.want)
			}
		})
	}
}

// swallowingPipeNote fires ONLY when the model declared verify=true (this is a build/test/run
// check) AND the command ends in a `| tail`/`| head` truncator that masks the exit code. It
// never fires on verify=false — the model's own intent replaces the old brittle command
// guess, which can't classify an arbitrary `./run.sh` and cried wolf on benign `ls | head`.
func TestSwallowingPipeNote(t *testing.T) {
	for _, tc := range []struct {
		name   string
		exit   int
		cmd    string
		verify bool
		want   bool
	}{
		{"verify make | tail", 0, "make world 2>&1 | tail -100", true, true},
		{"verify test | head", 0, "cargo test 2>&1 | head -50", true, true},
		{"verify script | tail", 0, "./run.sh | tail", true, true}, // arbitrary script — a guess couldn't classify it
		{"verify pipe upstream", 0, "make 2>&1 | grep -i error | tail -20", true, true},
		// verify=false → never fires, whatever the command (this is the false-positive fix).
		{"not verify: make | tail", 0, "make world 2>&1 | tail -100", false, false},
		{"not verify: ls | head", 0, "ls -la boot/ | head -20", false, false},
		{"not verify: git diff | head", 0, "cd /app && git diff | head -200", false, false},
		// verify but not a masking truncator, or non-zero exit → no note.
		{"verify grep filter (no truncator)", 0, "make 2>&1 | grep error", true, false},
		{"verify no pipe", 0, "make world", true, false},
		{"verify || not pipe", 0, "make || tail log", true, false},
		{"verify non-zero exit", 2, "make world 2>&1 | tail -100", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := swallowingPipeNote(tc.exit, tc.cmd, tc.verify) != ""
			if got != tc.want {
				t.Errorf("swallowingPipeNote(%d, %q, verify=%v) fired=%v, want %v", tc.exit, tc.cmd, tc.verify, got, tc.want)
			}
		})
	}
}

// ptyNeededNote fires for tty-gated commands (ssh/telnet/qemu-serial) launched WITHOUT a
// pty, and stays quiet for ssh-* non-interactive tools, scp, plain builds, and pty:true.
func TestPtyNeededNote(t *testing.T) {
	for _, tc := range []struct {
		name string
		cmd  string
		pty  bool
		want bool
	}{
		{"ssh host", "ssh alpine@localhost", false, true},
		{"ssh with flags", "ssh -p 2222 -i key root@127.0.0.1", false, true},
		{"telnet", "telnet 10.0.2.15 23", false, true},
		{"qemu -nographic", "qemu-system-x86_64 -m 512 -nographic -drive file=alpine.qcow2", false, true},
		{"qemu -serial stdio", "qemu-system-aarch64 -serial mon:stdio -kernel vmlinuz", false, true},
		{"in a && chain", "cd /app && ssh alpine@localhost", false, true},
		// pty already set → no nudge.
		{"ssh with pty", "ssh alpine@localhost", true, false},
		// Not interactive-tty-gated.
		{"ssh-keygen", "ssh-keygen -t ed25519 -f id", false, false},
		{"ssh-copy-id", "ssh-copy-id -i key root@host", false, false},
		{"scp", "scp file host:/tmp", false, false},
		{"plain build", "make world", false, false},
		{"qemu no serial", "qemu-img create -f qcow2 disk.qcow2 1G", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ptyNeededNote(tc.cmd, tc.pty) != ""
			if got != tc.want {
				t.Errorf("ptyNeededNote(%q, pty=%v) fired=%v, want %v", tc.cmd, tc.pty, got, tc.want)
			}
		})
	}
}

// backgroundTailNote fires on exit 0 when the command is `&`-detached (exit 0 means
// "started", not "finished"), never on `&&` lists, and warns harder when the same
// program is detached twice in one session (racing the in-flight copy).
func TestBackgroundTailNote(t *testing.T) {
	for _, tc := range []struct {
		name string
		exit int
		cmd  string
		want bool
	}{
		{"simple detach", 0, "opam install coq -y &", true},
		{"piped detach", 0, "opam install coq -y 2>&1 | tail -50 &", true},
		{"detach after cd", 0, "cd /tmp && make -j4 &", true},
		{"list operator not a detach", 0, "make && echo done", false},
		{"double ampersand terminal", 0, "make &&", false},
		{"mid-command detach only", 0, "server & curl localhost", false},
		{"no detach", 0, "opam install coq -y", false},
		{"non-zero exit", 1, "opam install coq -y &", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := backgroundTailNote(tc.exit, tc.cmd, session.SessionID("s-"+tc.name)) != ""
			if got != tc.want {
				t.Errorf("backgroundTailNote(%d, %q) fired=%v, want %v", tc.exit, tc.cmd, got, tc.want)
			}
		})
	}
}

// The second `&`-detach of the SAME program in one session gets the stronger
// duplicate-launch warning; a different program or a different session does not.
func TestBackgroundTailDuplicateLaunch(t *testing.T) {
	sid := session.SessionID("dup-test")
	first := backgroundTailNote(0, "opam switch create 4.14.2 -y &", sid)
	if strings.Contains(first, "ALREADY") {
		t.Fatalf("first launch must get the generic note, got %q", first)
	}
	second := backgroundTailNote(0, "opam install coq -y 2>&1 | tail -20 &", sid)
	if !strings.Contains(second, "ALREADY") || !strings.Contains(second, "`opam`") {
		t.Errorf("second opam detach must warn about the in-flight copy, got %q", second)
	}
	other := backgroundTailNote(0, "make -j4 &", sid)
	if strings.Contains(other, "ALREADY") {
		t.Errorf("a different program must not inherit the duplicate warning, got %q", other)
	}
	fresh := backgroundTailNote(0, "opam install coq -y &", session.SessionID("other-session"))
	if strings.Contains(fresh, "ALREADY") {
		t.Errorf("another session must start clean, got %q", fresh)
	}
}

// bgProgram digs the meaningful program out of wrappers, env prefixes, pipes, and
// leading cd segments — the name the duplicate warning keys on.
func TestBgProgram(t *testing.T) {
	for _, tc := range []struct{ cmd, want string }{
		{"opam install coq -y &", "opam"},
		{"cd /tmp && git clone x . &", "git"},
		{"FOO=1 nohup make -j4 &", "make"},
		{"timeout 300 opam init -n 2>&1 | tail -5 &", "opam"},
		{"sudo apt-get install -y coq &", "apt-get"},
	} {
		if got := bgProgram(tc.cmd); got != tc.want {
			t.Errorf("bgProgram(%q) = %q, want %q", tc.cmd, got, tc.want)
		}
	}
}

// End-to-end wiring: a `|| true`-masked crash gets the note directly after the status
// line (the head position both the model and the council's head-clip read); the off
// flag reproduces the un-annotated baseline; a genuine non-zero exit stays note-free.
func TestBashExecuteAnnotatesMaskedFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-specific masking idiom")
	}
	env := port.ToolEnv{Workdir: t.TempDir()}
	crash := `{"command":"printf 'Traceback (most recent call last):\\n  File x\\n'; false || true"}`

	r, _ := Bash{}.Execute(context.Background(), json.RawMessage(crash), env)
	out := resultText(t, r)
	if r.IsError {
		t.Fatalf("annotation must not reclassify: %s", out)
	}
	if !strings.HasPrefix(out, "exit 0\n[note: exit 0") {
		t.Errorf("note must sit right after the status line, got %q", out[:min(len(out), 120)])
	}

	// A silent masked failure (no crash text at all) still gets the structural
	// masking-tail note — the command string alone proves the exit is uninformative.
	r, _ = Bash{}.Execute(context.Background(), json.RawMessage(`{"command":"false || true"}`), env)
	out = resultText(t, r)
	if r.IsError || !strings.Contains(out, "masks the primary command's exit code") {
		t.Errorf("silent `|| true` mask must be annotated, got IsError=%v %q", r.IsError, out)
	}

	t.Setenv("MAGI_EXITCODE_BODYSCAN", "off")
	r, _ = Bash{}.Execute(context.Background(), json.RawMessage(crash), env)
	if out := resultText(t, r); strings.Contains(out, "[note:") {
		t.Errorf("off flag must reproduce the un-annotated baseline, got %q", out)
	}
	t.Setenv("MAGI_EXITCODE_BODYSCAN", "")

	r, _ = Bash{}.Execute(context.Background(),
		json.RawMessage(`{"command":"printf 'Traceback (most recent call last):\\n'; exit 3"}`), env)
	out = resultText(t, r)
	if !r.IsError || strings.Contains(out, "[note:") {
		t.Errorf("non-zero exit already speaks for itself — no note, IsError=true; got IsError=%v %q", r.IsError, out)
	}
}

// ephemeralEnvNote fires once per session on the first successful export/source —
// shell state is call-local, and an export-based "install" is a deliverable a fresh
// process cannot see. Bare VAR=val prefixes (correct single-command scoping) and
// non-zero exits stay silent; the second export in a session is not re-noted.
func TestEphemeralEnvNote(t *testing.T) {
	sid := session.SessionID("eph-1")
	if n := ephemeralEnvNote(0, `export PATH="/app/sqlite:$PATH" && sqlite3 --version`, sid); !strings.Contains(n, "fresh shell") {
		t.Fatalf("first export must be noted, got %q", n)
	}
	if n := ephemeralEnvNote(0, `export FOO=bar && make`, sid); n != "" {
		t.Errorf("second export in the same session must not repeat the note, got %q", n)
	}
	for _, tc := range []struct {
		name, cmd string
		exit      int
	}{
		{"bare env prefix", `CGO_ENABLED=0 go build ./...`, 0},
		{"cd only", `cd /app && make`, 0},
		{"export word in string", `echo "please export your data"`, 0}, // regex needs command position… echo's arg starts mid-word
		{"non-zero exit", `export PATH=/x && false`, 1},
	} {
		if n := ephemeralEnvNote(tc.exit, tc.cmd, session.SessionID("eph-"+tc.name)); tc.name != "export word in string" && n != "" {
			t.Errorf("%s: must not fire, got %q", tc.name, n)
		}
	}
	if n := ephemeralEnvNote(0, `source venv/bin/activate && pytest`, session.SessionID("eph-src")); !strings.Contains(n, "fresh shell") {
		t.Errorf("source must be noted, got %q", n)
	}
}
