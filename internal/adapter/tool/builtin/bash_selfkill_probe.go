package builtin

import (
	"context"
	"github.com/sayaya1090/magi/internal/quietconsole"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Asking the program that is about to run, instead of predicting it.
//
// The guard's question — "will this pkill hit us?" — has one exact answer, and it is not held by
// any regex library magi links. It is held by the pkill on the machine, whose matching is POSIX
// ERE through the platform's libc and differs from Go's RE2 in ways that are discovered one
// incident at a time. pgrep is that same program with the signal taken out: same source, same
// matcher, same libc, and it PRINTS what pkill would signal.
//
// So the probe rebuilds the invocation as pgrep, runs it, and looks for our own pid. Nothing is
// emulated, so nothing about ERE has to be known here — a backreference, an interval, a locale
// collating class and a doubled quantifier are all answered by the thing that will act on them.
//
// pgrep excludes itself from its own results, which is what is wanted: the question is whether
// MAGI is in the match, and magi is not the pgrep process.

// signalFlag matches the flags that say WHICH SIGNAL rather than which process: -9, -KILL,
// -SIGKILL, --signal=… . Everything else is a matching flag and is kept verbatim, because -f, -x,
// -i, -u and the rest change what the pattern covers and dropping one would make the probe answer
// a different question from the kill.
var signalFlag = regexp.MustCompile(`^-(?:[0-9]+|(?:SIG)?[A-Z]+[0-9]*)$`)

// probeTimeout bounds the probe. A machine whose process table takes seconds to walk is one where
// the kill is about to be slow too; the guard must not become the reason a turn stalls, so a probe
// that does not answer in time is treated as no answer and the caller falls back.
const probeTimeout = 3 * time.Second

// pgrepHitsUs reports whether pgrep, given the same matching flags and pattern, lists this
// process. ok is false when the question could not be put — no pgrep on the machine, or it did not
// answer — and the caller must then decide without it.
//
// Split out and injectable so the decision can be tested without a process table that happens to
// contain the right names.
var pgrepHitsUs = func(flags []string, pattern string) (hit, ok bool) {
	path, err := exec.LookPath("pgrep")
	if err != nil {
		return false, false
	}
	args := make([]string, 0, len(flags)+1)
	for _, f := range flags {
		if signalFlag.MatchString(f) || strings.HasPrefix(f, "--signal") {
			continue
		}
		args = append(args, f)
	}
	args = append(args, pattern)

	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	probe := exec.CommandContext(ctx, path, args...)
	quietconsole.Apply(probe)
	out, err := probe.Output()
	if ctx.Err() != nil {
		return false, false
	}
	// Exit 1 is pgrep's "nothing matched" and is an ANSWER, not a failure. Exit 2 and above mean it
	// could not read the pattern or the arguments — on macOS that is what `g++` produces — and that
	// is no answer at all.
	if err != nil {
		if ee, isExit := err.(*exec.ExitError); isExit && ee.ExitCode() == 1 {
			return false, true
		}
		return false, false
	}
	me := os.Getpid()
	for _, line := range strings.Fields(string(out)) {
		if pid, cerr := strconv.Atoi(line); cerr == nil && pid == me {
			return true, true
		}
	}
	return false, true
}
