package app

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/port"
)

// shellPlatform runs each command for real through /bin/sh, so a test of the read-only guard
// exercises the actual shell the guard is written against instead of a mock's idea of one.
type shellPlatform struct {
	mu   sync.Mutex
	cmds []string // every command as the platform received it (post-wrapping)
}

func (p *shellPlatform) Exec(ctx context.Context, c port.Cmd) (port.ExecResult, error) {
	p.mu.Lock()
	p.cmds = append(p.cmds, strings.Join(c.Args, " "))
	p.mu.Unlock()
	out, err := exec.CommandContext(ctx, c.Path, c.Args...).CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		return port.ExecResult{Stdout: out, ExitCode: 1}, nil
	}
	return port.ExecResult{Stdout: out, ExitCode: code}, nil
}
func (p *shellPlatform) ConfigDir() string           { return "" }
func (p *shellPlatform) DataDir() string             { return "" }
func (p *shellPlatform) TerminalCaps() port.TermCaps { return port.TermCaps{} }
func (p *shellPlatform) ProcessCPUTime(int) (time.Duration, bool) {
	return 0, false
}

func newShellApp(t *testing.T, plat port.Platform) *App {
	t.Helper()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return New(store, &gateLLM{text: "ok"}, builtin.Default(), bus.New(), plat, Config{Permission: "allow"})
}

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the guard is an sh prelude; the Windows check path runs powershell and is deliberately unwrapped")
	}
}

// The whole point of the guard: a "check" that re-does the step's work is refused by the shell it
// runs in, and a genuine read-only probe is untouched. Run against the real /bin/sh — the artifact
// under test is a shell string, so a mock would only test the test.
func TestReadOnlyShellRefusesWorkAndAllowsProbes(t *testing.T) {
	skipOnWindows(t)
	blocked := []string{
		"make world opt",                        // the live case: a check that rebuilt an entire compiler
		"make -C testsuite one DIR=tests/basic", // expensive even when it is "just" a test target
		"rm -rf /tmp/magi-ro-nope",
		"mv /tmp/magi-ro-a /tmp/magi-ro-b",
		"gcc -o /tmp/magi-ro-out /tmp/magi-ro-in.c",
		"git commit -m wip",
		"pip3 install requests",
		"apt install -y jq",
		"tar -czf /tmp/magi-ro.tgz /etc",
		"cargo build --release",
		"go build ./...",
		"npm install",
	}
	for _, cmd := range blocked {
		out, code := runSh(t, wrapReadOnly(cmd))
		if code != 126 {
			t.Errorf("%q: exit = %d, want 126 (refused)\n%s", cmd, code, out)
		}
		if blockedCommandIn(out) == "" {
			t.Errorf("%q: refusal carried no command name — the log could not say what was blocked:\n%s", cmd, out)
		}
	}

	// Read-only probes must survive untouched, including the read modes of dual-use commands: a guard
	// that also broke `git log` or `tar -tzf` would take away the very probes checks are told to use.
	allowed := map[string]int{
		"echo hi":                              0,
		"test -d /tmp":                         0,
		`python3 -c "import sys; sys.exit(0)"`: 0,
		"git --version":                        0,
		"pip3 --version":                       0,
		"grep -q . /etc/hosts":                 0,
		"tar -tzf /tmp/magi-ro-absent.tgz":     2, // ran for real and failed on the missing file, not refused
	}
	for cmd, want := range allowed {
		out, code := runSh(t, wrapReadOnly(cmd))
		if b := blockedCommandIn(out); b != "" {
			t.Errorf("%q was refused (%s) — read-only probes must pass through:\n%s", cmd, b, out)
		}
		if code == 126 {
			t.Errorf("%q: exit 126, want %d (not refused)\n%s", cmd, want, out)
		}
	}
}

func runSh(t *testing.T, cmd string) (string, int) {
	t.Helper()
	name, args := wfShell(cmd)
	out, err := exec.Command(name, args...).CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run %q: %v", cmd, err)
	}
	return string(out), code
}

// The prelude is prepended to EVERY check, so a syntax error in it would fail every check at once
// rather than the one bad command it targets. Guard that, and guard the stability the log depends on.
func TestReadOnlyPreambleParsesAndIsStable(t *testing.T) {
	skipOnWindows(t)
	name, _ := wfShell("")
	if out, err := exec.Command(name, "-n", "-c", readOnlyPreamble()).CombinedOutput(); err != nil {
		t.Fatalf("the prelude is not valid shell (%v):\n%s", err, out)
	}
	if readOnlyPreamble() != readOnlyPreamble() {
		t.Error("the prelude must be byte-stable across calls — map iteration order would make every log differ")
	}
	// g++/c++/clang++ cannot be shell function names. Emitting them would be a parse error taking down
	// every check, so they are skipped rather than blocked; assert the skip really happened.
	if strings.Contains(readOnlyPreamble(), "+") {
		t.Error("the prelude contains a `+`: a name sh cannot define as a function leaked into it")
	}
}

// A name sh cannot define as a function is not blocked — it is skipped. Listing one anyway would be a
// lie the preamble tells silently, so assert that every entry in the lists is actually shadowable.
// (`apt-get`, `g++`, `c++`, `clang++` are the known-unblockable names; they belong in the file's gap
// comment, not in the lists.)
func TestEveryBlockedNameIsShadowable(t *testing.T) {
	for _, c := range blockedAlways {
		if !shellFuncName(c) {
			t.Errorf("%q is listed as blocked but cannot be a shell function — it is silently skipped, not blocked", c)
		}
	}
	for c := range blockedSubcommands {
		if !shellFuncName(c) {
			t.Errorf("%q is listed as blocked but cannot be a shell function — it is silently skipped, not blocked", c)
		}
	}
}

func TestShellFuncName(t *testing.T) {
	for _, ok := range []string{"rm", "apt_get", "pip3", "_x"} {
		if !shellFuncName(ok) {
			t.Errorf("%q should be a usable shell function name", ok)
		}
	}
	for _, bad := range []string{"g++", "c++", "apt-get", "3pip", ""} {
		if shellFuncName(bad) {
			t.Errorf("%q must be rejected — sh cannot define it as a function", bad)
		}
	}
}

func TestWrapReadOnlyOffOrEmpty(t *testing.T) {
	t.Setenv("MAGI_CHECK_READONLY", "0")
	if got := wrapReadOnly("make all"); got != "make all" {
		t.Errorf("flag off must return the command verbatim, got %q", got)
	}
	t.Setenv("MAGI_CHECK_READONLY", "1")
	if got := wrapReadOnly("   "); got != "   " {
		t.Errorf("an empty command must not be given a prelude, got %q", got)
	}
	if !strings.Contains(wrapReadOnly("test -s f"), "test -s f") {
		t.Error("wrapping must keep the original command")
	}
}

func TestBlockedCommandIn(t *testing.T) {
	if got := blockedCommandIn("some output\n" + readOnlyBlockMarker + "make\nmore"); got != "make" {
		t.Errorf("blockedCommandIn = %q, want %q", got, "make")
	}
	if got := blockedCommandIn(readOnlyBlockMarker + "git commit"); got != "git commit" {
		t.Errorf("subcommand refusals must report both words, got %q", got)
	}
	if got := blockedCommandIn("ordinary failure output"); got != "" {
		t.Errorf("a non-refusal must report nothing, got %q", got)
	}
}

// 126 is "found but not executable" — a permission error, and now a read-only refusal. Both say
// nothing about the artifact, so both must read as "the check could not run".
func TestCheckUnrunnable(t *testing.T) {
	for _, code := range []int{126, 127} {
		if !checkUnrunnable(code) {
			t.Errorf("exit %d must count as the CHECK failing to run", code)
		}
	}
	for _, code := range []int{-1, 0, 1, 2} {
		if checkUnrunnable(code) {
			t.Errorf("exit %d must NOT be treated as unrunnable", code)
		}
	}
}

// End to end at the gate: a mutating check must not fail the step it gates. Before the guard it ran,
// re-doing the step's work every cycle; the danger in blocking it is the opposite error — recording a
// ✗ against a deliverable that is actually fine. It must instead yield NO verdict, and say so.
func TestBlockedCheckDoesNotFailTheStep(t *testing.T) {
	skipOnWindows(t)
	plat := &shellPlatform{}
	a := newShellApp(t, plat)
	s := parentSession(t.TempDir())
	sub := watchProgress(t, a, s.ID)
	a.mu.Lock()
	a.stateLocked(s.ID).deliverableChecks = []council.DeliverableCheck{
		{Step: "1", Deliverable: "the compiler builds", Command: "make world opt"},
	}
	a.mu.Unlock()

	ok, fails := a.verifyStepChecks(context.Background(), s, 0)
	if !ok {
		t.Errorf("a refused check must not gate the step (it proves nothing about the deliverable); fails=%q", fails)
	}
	if fails != "" {
		t.Errorf("a refused check must contribute no failure ledger, got %q", fails)
	}
	if note := sub.notes("check-readonly"); !strings.Contains(note, "make") {
		t.Errorf("the refusal must be reported by command name, got %q", note)
	}
	plat.mu.Lock()
	defer plat.mu.Unlock()
	if len(plat.cmds) != 1 || !strings.Contains(plat.cmds[0], readOnlyBlockMarker) {
		t.Errorf("the check must reach the platform WITH the prelude attached, got %v", plat.cmds)
	}
}

// The operator's own workflow verify command is not a model-authored check and is supposed to be able
// to build — it must keep reaching the shell unwrapped.
func TestWorkflowVerifyCommandIsNotSandboxed(t *testing.T) {
	skipOnWindows(t)
	plat := &shellPlatform{}
	a := newShellApp(t, plat)
	if _, code := a.runVerifyCmd(context.Background(), t.TempDir(), "echo built"); code != 0 {
		t.Fatalf("verify command exit = %d", code)
	}
	plat.mu.Lock()
	defer plat.mu.Unlock()
	if strings.Contains(plat.cmds[0], readOnlyBlockMarker) {
		t.Error("runVerifyCmd must stay unwrapped — it runs the operator's configured build/test command")
	}
}

// refusedCommandsIn is a PREDICTION of what the shell will refuse, and its only justification is that
// it agrees with the shell. So the table is checked twice: once for the names it reports, and once
// against the real prelude — a mismatch in either direction is the bug this exists to avoid (a missed
// refusal wastes a re-ask; an invented one sends the review chasing a check that runs fine).
func TestRefusedCommandsIn(t *testing.T) {
	cases := []struct {
		cmd  string
		want []string
	}{
		// the authoring mistake this exists for: a check that re-does the step's build
		{"cd ocaml && make world opt", []string{"make"}},
		{"make -C testsuite one DIR=tests/basic", []string{"make"}},
		{"./configure && make && ./run", []string{"make"}},
		// a path invocation is not shadowed by the shell, but it still builds — report it
		{"/usr/bin/make all", []string{"make"}},
		// env assignments precede the command word
		{"CC=gcc make lib", []string{"make"}},
		// dual-use commands turn on the FIRST ARGUMENT, exactly as the preamble's `case "${1:-}"` does
		{"git commit -m x", []string{"git commit"}},
		{"git log -1 --oneline", nil},
		{"git -C repo commit -m x", nil}, // $1 is "-C": the preamble lets this through, so must this
		{"pip install requests", []string{"pip install"}},
		{"pip show requests", nil},
		// tar has a genuinely read-only mode
		{"tar -tzf dist.tgz", nil},
		{"tar -czf dist.tgz build/", []string{"tar (create/extract)"}},
		{"tar cf dist.tar build/", []string{"tar (create/extract)"}},
		// several command positions, reported once each in order
		{"rm -f out.log; make build; make build", []string{"rm", "make"}},
		// a substitution hides a command position
		{"test -n \"$(make -s print-version)\"", []string{"make"}},
		// genuine read-only probes: the common shapes a check should have
		{"grep -q PATTERN out.log", nil},
		{"test -f bin/app && test -s bin/app", nil},
		{"./bin/app --version | grep -q 1.2.3", nil},
		{"python3 -c \"import socket; socket.create_connection(('127.0.0.1',5328))\"", nil},
		// a blocked NAME inside a quoted argument is not an invocation
		{"grep -q 'rm -rf' script.sh", nil},
		{"echo \"make world\"", nil},
		// unnameable in sh (see shellFuncName): not shadowed, so not predicted
		{"g++ -o app main.cc", nil},
		{"", nil},
	}
	for _, c := range cases {
		got := refusedCommandsIn(c.cmd)
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("refusedCommandsIn(%q) = %v, want %v", c.cmd, got, c.want)
		}
	}
}

// The prediction must match the prelude it mirrors. Run each command through the real shell and
// compare "predicted refused" with "actually refused" — the assertion the table alone cannot make.
// Only commands that are safe to actually execute here are listed: the refused ones never run their
// real binary (that is the point), and the allowed ones are read-only probes.
func TestRefusedCommandsInMatchesTheShell(t *testing.T) {
	skipOnWindows(t)
	a := newShellApp(t, &shellPlatform{})
	for _, cmd := range []string{
		"cd /tmp && make world",
		"git commit -m x",
		"tar -czf /tmp/magi-test.tgz .",
		"cmake --build .",
		"NOPE=1 rm -rf /tmp/magi-does-not-exist",
		"grep -q root /etc/passwd",
		"test -f go.mod",
		"git status --porcelain",
		"echo 'rm -rf /'",
		"tar -tf /tmp/magi-does-not-exist.tgz",
	} {
		out, code := a.runCheckCmd(context.Background(), "s_test", t.TempDir(), cmd)
		actual := blockedCommandIn(out) != ""
		if predicted := len(refusedCommandsIn(cmd)) > 0; predicted != actual {
			t.Errorf("%q: predicted refused=%v but the shell refused=%v (exit %d, out %q)",
				cmd, predicted, actual, code, strings.TrimSpace(out))
		}
		if actual && code != 126 {
			t.Errorf("%q: a refusal must exit 126 so the gates read it as unrunnable, got %d", cmd, code)
		}
	}
}
