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
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// selfKillGuardEnabled gates the block (MAGI_SELFKILL_GUARD, default ON).
func selfKillGuardEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAGI_SELFKILL_GUARD"))) {
	case "0", "off", "false", "no":
		return false
	}
	return true
}

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

// patternMatches applies pat as a regex (pkill semantics), falling back to a
// substring test when it doesn't compile.
func patternMatches(pat, target string) bool {
	if pat == "" || target == "" {
		return false
	}
	if re, err := regexp.Compile(pat); err == nil {
		return re.MatchString(target)
	}
	return strings.Contains(target, pat)
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
