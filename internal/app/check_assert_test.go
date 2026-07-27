package app

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/session"
)

func TestParseAssertion(t *testing.T) {
	cases := []struct {
		in       string
		wantVerb string
		wantArg  string
		wantOK   bool
	}{
		{"nonempty", "nonempty", "", true},
		{"NONEMPTY", "nonempty", "", true},
		{"  nonempty  ", "nonempty", "", true},
		{"process_alive", "process_alive", "", true},
		// The argument is the REST of the string: a regexp with spaces in it is the common case, and a
		// Fields-based parse would silently keep only its first word.
		{"matches ^All 12 tests passed$", "matches", "^All 12 tests passed$", true},
		{"absent  Traceback (most recent call last)", "absent", "Traceback (most recent call last)", true},
		{"equals /tmp/expected out.txt", "equals", "/tmp/expected out.txt", true},
		{"port_open 5328", "port_open", "5328", true},
		// Verbs that mean nothing without their argument, and anything outside the vocabulary, are a
		// parse failure — never a guess at some other assertion.
		{"matches", "", "", false},
		{"equals", "", "", false},
		{"port_open", "", "", false},
		{"", "", "", false},
		{"exists /tmp/x", "", "", false},
		{"test -f /tmp/x", "", "", false},
	}
	for _, tc := range cases {
		got, ok := parseAssertion(tc.in)
		if ok != tc.wantOK || got.verb != tc.wantVerb || got.arg != tc.wantArg {
			t.Errorf("parseAssertion(%q) = {%q %q} %v, want {%q %q} %v",
				tc.in, got.verb, got.arg, ok, tc.wantVerb, tc.wantArg, tc.wantOK)
		}
	}
}

// runTypedCheck must speak the exit-code language the gates already layer policy on: 0 passed,
// 1 the deliverable failed, 126 the CHECK could not run (checkUnrunnable → no verdict, step lands
// ungated). Getting 1 and 126 the wrong way round would either fail correct work or wave broken
// work through, so each is pinned.
func TestRunTypedCheckVerdicts(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	log := write("build.log", "compiling\nAll 12 tests passed\n")
	write("copy.log", "compiling\nAll 12 tests passed\n")
	write("empty.log", "   \n")

	app := newShellApp(t, &shellPlatform{})
	// The verbs are exercised against a file the RUN produced, because that is the only shape the
	// check contract asks for ("produce that file as the REAL output of the work"). `absent` reads
	// its subject's history as well as its contents — a pattern missing from a file nothing touched
	// is a fact about the file, not about the step — so the record has to say the run wrote it or
	// this table would be testing a case the contract forbids.
	sid, cerr := app.CreateSession(context.Background(), command.CreateSession{Workdir: dir})
	if cerr != nil {
		t.Fatal(cerr)
	}
	if _, err := app.store.Append(context.Background(), sid,
		toolCallEvent("bash", `{"command":"make world > `+filepath.ToSlash(log)+` 2>&1"}`)); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		check  council.DeliverableCheck
		want   int
		inText string // substring the diagnostic must carry
	}{
		{"nonempty pass", council.DeliverableCheck{Source: log, Assert: "nonempty"}, 0, "bytes"},
		{"nonempty fail on whitespace", council.DeliverableCheck{Source: filepath.Join(dir, "empty.log"), Assert: "nonempty"}, 1, "is empty"},
		{"matches pass", council.DeliverableCheck{Source: log, Assert: "matches All 12 tests passed"}, 0, "matches"},
		{"matches fail", council.DeliverableCheck{Source: log, Assert: "matches All 13 tests passed"}, 1, "does not match"},
		// An anchored pattern against a file ending in a newline is the permanent-false-failure shape
		// the shared matcher was fixed for; the typed path must inherit that fix, not re-earn it.
		{"matches anchored despite trailing newline", council.DeliverableCheck{Source: log, Assert: "matches ^All 12 tests passed$"}, 0, "matches"},
		{"absent pass", council.DeliverableCheck{Source: log, Assert: "absent Traceback"}, 0, "does not contain"},
		{"absent fail", council.DeliverableCheck{Source: log, Assert: "absent compiling"}, 1, "must be absent"},
		{"equals pass", council.DeliverableCheck{Source: log, Assert: "equals " + filepath.Join(dir, "copy.log")}, 0, "equals"},
		{"equals fail", council.DeliverableCheck{Source: log, Assert: "equals " + filepath.Join(dir, "empty.log")}, 1, "differs"},
		// A source the step never recorded is the DELIVERABLE failing, not a broken check: the step was
		// supposed to put it there. Reporting 126 would silently un-gate the step.
		{"missing source fails", council.DeliverableCheck{Source: filepath.Join(dir, "nope.log"), Assert: "nonempty"}, 1, "record it here"},
		{"missing equals target fails", council.DeliverableCheck{Source: log, Assert: "equals " + filepath.Join(dir, "nope.log")}, 1, "record it here"},
		// The check itself is unusable → 126, no verdict.
		{"unknown verb is unrunnable", council.DeliverableCheck{Source: log, Assert: "exists"}, 126, "unknown assertion"},
		{"file assertion with no source is unrunnable", council.DeliverableCheck{Assert: "nonempty"}, 126, "needs `source`"},
		{"port_open with a non-port is unrunnable", council.DeliverableCheck{Assert: "port_open http"}, 126, "needs a port number"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, code := app.runTypedCheck(context.Background(), sid, dir, tc.check)
			if code != tc.want {
				t.Fatalf("code = %d, want %d (out: %s)", code, tc.want, out)
			}
			if !strings.Contains(out, tc.inText) {
				t.Fatalf("out = %q, want it to mention %q", out, tc.inText)
			}
		})
	}
}

// A path with a shell metacharacter in it is an ordinary argv element, not a command line: the whole
// point of building the invocation here is that there is nothing for it to break out of.
func TestRunTypedCheckPathWithMetacharacters(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	name := "out; rm -rf $(pwd) & '.log"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("ok\n"), 0o644); err != nil {
		t.Skipf("filesystem will not hold this name: %v", err)
	}
	app := newShellApp(t, &shellPlatform{})
	out, code := app.runTypedCheck(context.Background(), session.SessionID("s1"), dir,
		council.DeliverableCheck{Source: filepath.Join(dir, name), Assert: "matches ok"})
	if code != 0 {
		t.Fatalf("code = %d, want 0 (out: %s)", code, out)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("workdir gone — the path was interpreted rather than passed: %v", err)
	}
}

func TestRunTypedCheckPortOpen(t *testing.T) {
	skipOnWindows(t)
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3 to run the probe")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	openPort := ln.Addr().(*net.TCPAddr).Port

	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := closed.Addr().(*net.TCPAddr).Port
	closed.Close()

	app := newShellApp(t, &shellPlatform{})
	if out, code := app.runTypedCheck(context.Background(), session.SessionID("s1"), t.TempDir(),
		council.DeliverableCheck{Assert: "port_open " + strconv.Itoa(openPort)}); code != 0 {
		t.Fatalf("open port: code = %d, want 0 (out: %s)", code, out)
	}
	// Liveness is the one contract a recorded artifact cannot prove, so this must actually probe:
	// a closed port has to come back failing, not merely "no verdict".
	if out, code := app.runTypedCheck(context.Background(), session.SessionID("s1"), t.TempDir(),
		council.DeliverableCheck{Assert: "port_open " + strconv.Itoa(closedPort)}); code != 1 {
		t.Fatalf("closed port: code = %d, want 1 (out: %s)", code, out)
	}
}

// runCheck is the single entry point the gates call, and there is no command path behind it any more:
// a leftover `command` is inert data, and a check that carries no assertion is reported and yields no
// verdict (126) rather than being executed.
func TestRunCheckIsTypedOnly(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "r.log"), []byte("done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := newShellApp(t, &shellPlatform{})
	typed := council.DeliverableCheck{
		Source: filepath.Join(dir, "r.log"), Assert: "matches done",
		Command: "exit 3", // present but inert — nothing executes a model-authored command
	}
	if out, code := app.runCheck(context.Background(), session.SessionID("s1"), dir, typed); code != 0 {
		t.Fatalf("typed: code = %d, want 0 (out: %s)", code, out)
	}
	// No assertion: the runner has nothing to evaluate. It must NOT fall back to the command — that
	// would put the whole authored-shell surface back — and must not fail the deliverable either.
	untyped := council.DeliverableCheck{Deliverable: "the suite passes", Command: "exit 3"}
	out, code := app.runCheck(context.Background(), session.SessionID("s1"), dir, untyped)
	if code != 126 {
		t.Fatalf("no assertion: code = %d, want 126 (no verdict), out: %s", code, out)
	}
	if !strings.Contains(out, "no assertion") {
		t.Errorf("the reason must say the check asserts nothing, got %q", out)
	}
}

// A typed check's verdict IS the runner's exit status — the runner applied the assertion itself. A
// stale `expect` left on a converted check must not be re-applied on top, or a pattern meant for some
// other command's output would fail work the assertion just passed.
func TestPassesIgnoresExpectOnTypedCheck(t *testing.T) {
	c := council.DeliverableCheck{Source: "/tmp/x", Assert: "nonempty", Expect: "a pattern from the old command"}
	if !c.Passes("/tmp/x: 41 bytes", 0) {
		t.Fatal("a typed check that exited 0 must pass regardless of a leftover expect")
	}
	if c.Passes("whatever", 1) {
		t.Fatal("a non-zero exit must still fail")
	}
}

// Every map in the run that identifies a check used to key on the command text. A typed check has
// none, so keying on it alone collapses a step's typed checks onto one entry — and the first to pass
// marks the rest green.
func TestCheckIdentDistinguishesTypedChecks(t *testing.T) {
	a := council.DeliverableCheck{Step: "2", Source: "/tmp/build.log", Assert: "matches ^Done"}
	b := council.DeliverableCheck{Step: "2", Source: "/tmp/build.log", Assert: "absent error"}
	c := council.DeliverableCheck{Step: "2", Source: "/tmp/test.log", Assert: "matches ^Done"}
	if checkKey(a) == checkKey(b) || checkKey(a) == checkKey(c) {
		t.Fatalf("typed checks collided: %q %q %q", checkKey(a), checkKey(b), checkKey(c))
	}
	if checkIdent(council.DeliverableCheck{}) != "" {
		t.Fatal("a check that verifies nothing must have no identity, so callers drop it")
	}
	// A command check keeps its old identity exactly, so nothing that was stable before moves.
	if got := checkIdent(council.DeliverableCheck{Command: " make test "}); got != "make test" {
		t.Fatalf("command identity = %q", got)
	}
}

func TestCheckWhatDescribesTypedCheck(t *testing.T) {
	if got := checkWhat(council.DeliverableCheck{Source: "/tmp/b.log", Assert: "matches ^Done"}); got != "/tmp/b.log: matches ^Done" {
		t.Fatalf("checkWhat = %q", got)
	}
	if got := checkWhat(council.DeliverableCheck{Command: "make test"}); got != "make test" {
		t.Fatalf("checkWhat = %q", got)
	}
}

// unionChecks and parseChecksArray both used "no command" to mean "nothing to run". A typed check
// satisfies that test while being perfectly runnable, and either one dropping it silently would
// leave the plan with fewer gates than it authored.
func TestTypedChecksSurviveUnionAndParse(t *testing.T) {
	authored := []council.DeliverableCheck{{Step: "1", Source: "/tmp/a.log", Assert: "nonempty"}}
	out, restored := unionChecks(nil, authored)
	if restored != 1 || len(out) != 1 {
		t.Fatalf("unionChecks restored %d, out %+v — a typed check was dropped", restored, out)
	}
	if _, again := unionChecks(out, authored); again != 0 {
		t.Fatal("the same typed check was restored twice — its identity is not stable")
	}
	got, ok := parseChecksArray(`[{"step":"1","deliverable":"d","source":"/tmp/a.log","assert":"matches ^Done$"}]`)
	if !ok || len(got) != 1 || got[0].Assert != "matches ^Done$" || got[0].Source != "/tmp/a.log" {
		t.Fatalf("parseChecksArray = %+v ok=%v", got, ok)
	}
}

// A source that never yields — a fifo nobody writes to, a device file — used to be bounded because
// every check ran through runVerifyCmd's per-command deadline. The typed runner reaches the platform
// directly, so it has to carry that bound itself; without it one ill-authored check strands the turn
// until its wall clock. And the kill must land as "no verdict" (126): a killed reader exits by
// signal, which by exit code alone is indistinguishable from an unreadable file, so a deadline would
// otherwise be reported as the deliverable failing.
func TestRunTypedCheckBoundsABlockingSource(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	fifo := filepath.Join(dir, "build.log")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("no fifo here: %v", err)
	}
	t.Setenv("MAGI_CHECK_TIMEOUT", "1")

	app := newShellApp(t, &shellPlatform{})
	done := make(chan struct{})
	var out string
	var code int
	go func() {
		defer close(done)
		out, code = app.runTypedCheck(context.Background(), session.SessionID("s1"), dir,
			council.DeliverableCheck{Source: fifo, Assert: "matches passed"})
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("a blocking source ran past the per-check deadline — the bound is not applied")
	}
	if code != 126 {
		t.Fatalf("code = %d, want 126 (a check that never answered says nothing about the artifact); out: %s", code, out)
	}
	if !strings.Contains(out, "did not finish") {
		t.Errorf("out = %q, want it to say the read did not finish", out)
	}
}
