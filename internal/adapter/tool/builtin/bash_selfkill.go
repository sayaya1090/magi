package builtin

// Self-kill protection for the bash tool, cross-platform. Kill-by-match commands
// can hit the agent's OWN process and end the whole run (observed live: a
// `pkill -9 -f "release"` matched the word "release" inside magi's task prompt —
// exit 137, everything lost). Unlike the advisory notes in bash_notes.go this
// check BLOCKS: self-termination is unrecoverable, so a note the model would
// read after dying is no defense. The block is exact — it fires only when the
// target demonstrably matches this process's own command line or binary name,
// so a kill that cannot hit us always passes.
//
// Covered forms:
//   - Unix:    `pkill -f <pattern>`    (pattern vs our full command line)
//   - Unix:    `pkill <pattern>` / `killall <name>` (vs our process name)
//   - Windows: `taskkill … /IM <name>` and `Stop-Process -Name <name>`
//     (PowerShell is the shell there; names may carry .exe and * wildcards)
//
// Killing by PID is never blocked — a PID the agent read from pgrep/ps/Get-Process
// is a deliberate, precise target.

import (
	"github.com/sayaya1090/magi/internal/envflag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// selfKillGuardEnabled gates the block (MAGI_SELFKILL_GUARD, default ON).
func selfKillGuardEnabled() bool { return envflag.Enabled("MAGI_SELFKILL_GUARD", true) }

var (
	// pkill/killall [flags] <pattern> — flags may be separate (-9 -f) or combined (-9f).
	pkillInvocation = regexp.MustCompile(`\b(pkill|killall)\s+((?:-\S+\s+)*)("[^"]*"|'[^']*'|\S+)`)
	// taskkill … /IM <name> (case-insensitive, PowerShell/cmd).
	taskkillIM = regexp.MustCompile(`(?i)\btaskkill\b[^;|&\n]*?/IM\s+("[^"]*"|'[^']*'|\S+)`)
	// Stop-Process -Name/-ProcessName <name[,name…]> (PowerShell).
	stopProcessName = regexp.MustCompile(`(?i)\bStop-Process\b[^;|&\n]*?-(?:Process)?Name\s+("[^"]*"|'[^']*'|\S+)`)
)

// selfKillReason returns a non-empty refusal when command contains a kill-by-match
// whose target matches selfCmdline (this process's full command line) or selfName
// (this process's binary name, extension stripped). Callers pass the real values;
// tests inject their own.
func selfKillReason(command, selfCmdline, selfName string) string {
	refuse := func(target string) string {
		return "blocked: this kill command's target (" + target + ") matches this agent's OWN process — running it would kill the agent itself and lose the whole run. Kill the intended process precisely instead: by PID (from pgrep/ps or Get-Process), by exact unrelated name, or narrow the pattern so it cannot match this agent."
	}
	for _, m := range pkillInvocation.FindAllStringSubmatch(command, -1) {
		verb, flags, pat := m[1], m[2], strings.Trim(m[3], `"'`)
		// The real matcher, asked first and trusted in ONE DIRECTION. pgrep is this same program
		// with the signal removed (see bash_selfkill_probe.go), so when it lists this process the
		// kill would reach it — that is not a prediction and it ends the question.
		//
		// Its silence is not the same fact. Measured on this machine: `pgrep -f builtin.test` finds
		// nothing while a process with exactly that in its argv is running. Whatever the cause, a
		// matcher that can miss a process that IS there cannot be allowed to clear a kill, or the
		// guard would be weaker than the plain pattern check it replaced. So a miss falls through
		// and everything below still runs.
		if verb == "pkill" {
			if hit, _ := pgrepHitsUs(strings.Fields(flags), pat); hit {
				return refuse("`pkill " + strings.TrimSpace(flags+" "+pat) + "` — pgrep lists this process")
			}
		}
		if verb == "pkill" && strings.Contains(flags, "f") {
			// -f matches the FULL command line (task prompt included).
			if patternMatches(pat, selfCmdline) {
				return refuse("`pkill -f " + pat + "` vs our command line")
			}
			continue
		}
		// pkill <pat> is a regex over process names; killall <name> is a name match.
		if patternMatches(pat, selfName) {
			return refuse("`" + verb + " " + pat + "` vs our process name")
		}
	}
	for _, re := range []*regexp.Regexp{taskkillIM, stopProcessName} {
		for _, m := range re.FindAllStringSubmatch(command, -1) {
			for _, name := range strings.Split(strings.Trim(m[1], `"'`), ",") {
				if windowsNameMatches(strings.TrimSpace(name), selfName) {
					return refuse("`" + strings.TrimSpace(name) + "` vs our process name")
				}
			}
		}
	}
	return ""
}

// patternMatches answers whether a kill pattern covers this process.
//
// # Why this does not try to be a regex engine
//
// It used to compile the pattern with Go's regexp and, when that failed, ask whether the target
// CONTAINED the pattern as a literal. That is emulation, and it was wrong in the direction that
// costs a run. Go's regexp is RE2 and refuses a doubled quantifier — `regexp.Compile("g++")` fails
// with "invalid nested repetition operator" — while the pkill actually about to run is POSIX ERE
// through the platform's libc. Measured 2026-08-18: on Linux `echo magi | grep -E 'g++'` MATCHES
// (the second + is read as a repeat of `g+`, so the pattern is any name holding a "g"); on macOS
// the same pattern is refused outright. A trial rebuilding Caffe swept its compilers with
// `pkill -9 g++`, this guard passed it, and the binary — magi-amd64 — was inside the match.
//
// Doubled quantifiers are only the case that was caught. RE2 and POSIX ERE also disagree about
// backreferences, some interval forms, empty alternations and locale collation, and each of those
// is another pattern this could wave through. Enumerating them is a list that is wrong until the
// next one is found.
//
// So the question goes to the program that will answer it for real. pgrep IS pkill without the
// signal — same source, same matcher, same libc — so running the pattern through pgrep and looking
// for our own pid is not an approximation of what the kill will do. It is what the kill will do.
//
// Go's regexp stays as the fallback for a machine with no pgrep (stripped containers, which this
// tree has met: pkill/pgrep/lsof/ss all exit 127 there). A pattern that Go cannot read there is
// refused rather than assumed harmless, because "I cannot tell" is not "probably fine" — the
// refusal names the alternative, which is killing by pid.
func patternMatches(pat, target string) bool {
	if pat == "" || target == "" {
		return false
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		// Unreadable here and readable to the shell is exactly the case that got through.
		return true
	}
	return re.MatchString(target)
}

// windowsNameMatches compares a taskkill/Stop-Process name (optionally with .exe
// and * wildcards) against our binary name, case-insensitively.
func windowsNameMatches(name, selfName string) bool {
	name = strings.TrimSuffix(strings.ToLower(name), ".exe")
	self := strings.TrimSuffix(strings.ToLower(selfName), ".exe")
	if name == "" || self == "" {
		return false
	}
	if !strings.Contains(name, "*") {
		return name == self
	}
	re, err := regexp.Compile("^" + strings.ReplaceAll(regexp.QuoteMeta(name), `\*`, ".*") + "$")
	return err == nil && re.MatchString(self)
}

// selfIdentity returns this process's full command line and binary name for the
// guard. os.Args covers both platforms; the name comes from argv[0]'s base.
func selfIdentity() (cmdline, name string) {
	return strings.Join(os.Args, " "), filepath.Base(os.Args[0])
}
