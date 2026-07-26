package app

import (
	"runtime"
	"strings"
)

// A deliverable check must VERIFY the produced artifact, never PERFORM the step's work. That rule
// lived only in the authoring prompts and in the LLM check-audit, and both are advisory: a live run
// authored four "checks" that each rebuilt an entire compiler (`make`, `make world opt`,
// `./configure && make world opt`), the audit replied with an empty array — so they were stored
// UNREVIEWED — and every gate cycle then re-ran the build the step had just done.
//
// This file makes the rule deterministic at the point of execution: a check runs in a shell where
// the mutating commands are shadowed by functions that refuse. A refusal exits 126, which the gates
// read as "the CHECK could not run" (see checkUnrunnable) rather than "the deliverable failed", so a
// mutating check yields NO verdict instead of false-failing correct work, and the existing
// substitution flow can ask for a working read-only equivalent.
//
// Scope, stated honestly: this shadows commands resolved BY NAME through the shell. It does not stop
// an absolute path (`/bin/rm`), a `>` redirect, or a mutation performed inside `python3 -c`. It is a
// guard against the authoring mistake that actually happens — a check written as a plain build or
// cleanup command — not a sandbox against a hostile check. The authoring prompts carry the same rule
// so the two layers agree.
//
// One further gap is forced by the shell itself: a name that is not a POSIX identifier cannot be
// defined as a function. `apt-get`, `g++`, `c++` and `clang++` are therefore UNBLOCKABLE here — dash
// rejects the definition outright ("Bad function name") and a rejected definition would be a parse
// error taking every check down with it, so they are deliberately absent from the lists below rather
// than listed and silently skipped. `apt`/`gcc`/`clang` cover the common spellings; the hyphenated and
// `++` ones stay unguarded, and shellFuncName is the assertion that keeps a future addition from
// quietly believing otherwise.

// readOnlyChecksEnabled gates the read-only check shell (default ON; MAGI_CHECK_READONLY=0 restores
// the unguarded behavior for an A/B baseline).
func readOnlyChecksEnabled() bool { return !envOff("MAGI_CHECK_READONLY") }

// readOnlyBlockMarker is printed by every refusal. runCheckCmd looks for it so a blocked check is
// reported by NAME — an exit code alone leaves nobody able to tell which command was refused.
const readOnlyBlockMarker = "magi-check-readonly: blocked "

// blockedAlways are commands with no read-only use inside a check: they build, install, fetch,
// destroy, or rewrite the artifact. A check needs none of them to inspect something already made.
// Package managers are listed whole (no subcommand of one belongs in a check); compilers are listed
// because compiling IS producing, and "it still builds" is a precondition rather than proof of the
// contract. Every entry must satisfy shellFuncName — see the gap noted at the top of the file.
var blockedAlways = []string{
	// destructive / relocating
	"rm", "rmdir", "unlink", "mv", "shred", "truncate", "dd", "chmod", "chown",
	// build drivers
	"make", "gmake", "cmake", "ninja", "meson", "bazel", "gradle", "mvn", "ant", "scons",
	// compilers and linkers
	"gcc", "cc", "clang", "ld", "ar", "rustc", "javac", "tsc",
	// fetch / copy across a boundary
	"wget", "scp", "rsync",
	// archive create/extract (tar is handled separately: it has a read-only list mode)
	"zip", "unzip", "gzip", "gunzip", "bzip2", "bunzip2", "xz", "unxz",
	// package managers
	"apt", "yum", "dnf", "apk", "pacman", "brew", "easy_install",
}

// blockedSubcommands are dual-use commands: the first word decides. `git log` reads, `git commit`
// writes; `pip show` reads, `pip install` writes. Blocking the whole command would take away probes
// a check legitimately needs, so only the writing subcommands are refused and everything else is
// passed through to the real binary.
var blockedSubcommands = map[string][]string{
	"git": {"add", "am", "apply", "checkout", "cherry-pick", "clean", "clone", "commit", "fetch",
		"init", "merge", "mv", "pull", "push", "rebase", "reset", "restore", "revert", "rm",
		"stash", "submodule", "switch", "tag"},
	"pip":    {"install", "uninstall", "download", "wheel"},
	"pip3":   {"install", "uninstall", "download", "wheel"},
	"npm":    {"install", "ci", "i", "add", "remove", "uninstall", "update", "run", "exec", "publish"},
	"yarn":   {"install", "add", "remove", "upgrade", "run", "publish"},
	"pnpm":   {"install", "i", "add", "remove", "update", "run", "publish"},
	"go":     {"build", "install", "get", "generate", "clean", "mod"},
	"cargo":  {"build", "install", "fetch", "clean", "update", "run", "publish"},
	"docker": {"build", "run", "rm", "rmi", "create", "start", "compose"},
}

// readOnlyPreamble is the shell prelude prepended to every check. POSIX sh throughout (the platform
// shell may be dash): functions plus `case`, and `command` to reach the real binary when a dual-use
// subcommand is allowed through.
func readOnlyPreamble() string {
	var b strings.Builder
	// The refusal is recorded in a FILE as well as on stderr, because stderr does not survive the check
	// itself: `make … >/dev/null 2>&1 && echo passed || echo failed` discards the message and then
	// replaces the 126 with exit 0, so the refusal reached nobody and the check landed as a deliverable
	// FAILURE. A file is outside the command's redirections; the trailer reads it back after the command
	// has finished and its own redirections no longer apply. If the flag file cannot be created (a
	// read-only filesystem), everything degrades to the stderr marker alone — the previous behavior.
	b.WriteString("__magi_ro_flags=\"${TMPDIR:-/tmp}/.magi-ro-$$\"\n")
	b.WriteString(": > \"$__magi_ro_flags\" 2>/dev/null\n")
	b.WriteString("__magi_ro_block() { printf '%s%s\\n' '" + readOnlyBlockMarker + "' \"$1\" >&2; " +
		"printf '%s\\n' \"$1\" >> \"$__magi_ro_flags\" 2>/dev/null; return 126; }\n")
	for _, c := range blockedAlways {
		if !shellFuncName(c) {
			continue // not expressible as a shell function name (g++, c++, clang++) — see below
		}
		b.WriteString(c + "() { __magi_ro_block " + c + "; }\n")
	}
	for _, c := range sortedKeys(blockedSubcommands) {
		subs := blockedSubcommands[c]
		b.WriteString(c + "() { case \"${1:-}\" in " + strings.Join(subs, "|") +
			") __magi_ro_block '" + c + " '\"$1\";; *) command " + c + " \"$@\";; esac; }\n")
	}
	// tar is the one command here with a genuinely read-only mode. `-t`/`--list` inspects an archive
	// (exactly what a check of "the tarball contains X" should do); create and extract both write, so
	// they are refused. Anything else falls through to the real tar rather than guessing.
	b.WriteString("tar() { case \" $* \" in *' -c'*|*' --create'*|*' -x'*|*' --extract'*|' c'*|' x'*) " +
		"__magi_ro_block 'tar (create/extract)';; *) command tar \"$@\";; esac; }\n")
	return b.String()
}

// shellFuncName reports whether name can be defined as a POSIX shell function. Compilers spelled with
// `+` (g++, c++, clang++) cannot be — sh rejects the definition and, worse, a parse error would take
// the whole check down with it. They are skipped: a check that shells out to g++ directly stays
// unguarded, which is the honest limit of a name-shadowing guard.
func shellFuncName(name string) bool {
	for i := 0; i < len(name); i++ {
		c := name[i]
		ok := c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (i > 0 && c >= '0' && c <= '9')
		if !ok {
			return false
		}
	}
	return name != ""
}

// sortedKeys keeps the generated preamble byte-identical across runs — map order would otherwise make
// the prelude (and so any log or test comparing it) differ call to call.
func sortedKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ { // insertion sort: the map is a handful of entries
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// wrapReadOnly returns cmd prefixed with the refusal prelude. It is a no-op when the guard is off,
// when the command is empty, and on Windows — there the check runs through powershell (wfShell), for
// which this sh prelude is not merely useless but a syntax error that would fail every check.
func wrapReadOnly(cmd string) string {
	if !readOnlyChecksEnabled() || strings.TrimSpace(cmd) == "" || runtime.GOOS == "windows" {
		return cmd
	}
	return readOnlyPreamble() + cmd + "\n" + readOnlyTrailer()
}

// readOnlyTrailer runs after the check's own command and republishes any refusal on STDOUT, where no
// redirection inside the check can have reached it, and restores the 126 the check may have overwritten.
// Without it the guard is only as reliable as the command's own error handling, which is exactly the
// thing a broken check gets wrong. `command rm` is required: rm is shadowed by the preamble above.
func readOnlyTrailer() string {
	return "__magi_ro_rc=$?\n" +
		"if [ -s \"$__magi_ro_flags\" ]; then printf '%s%s\\n' '" + readOnlyBlockMarker +
		"' \"$(head -n 1 \"$__magi_ro_flags\")\"; __magi_ro_rc=126; fi\n" +
		"command rm -f \"$__magi_ro_flags\" 2>/dev/null\n" +
		"exit $__magi_ro_rc\n"
}

// checkUnrunnable reports whether an exit code means the CHECK itself could not run, as opposed to
// the deliverable failing. 127 is "command not found" (a missing tool or a wrong path). 126 is "found
// but not executable" — a permission error, and now also the read-only guard's refusal. Both say
// nothing about the artifact, so every gate skips them rather than reworking correct code; 126 used
// to be counted as a failure at each of those sites, which false-failed a deliverable whenever a
// check hit a non-executable file. subst_review already paired the two, and this makes the gates agree.
func checkUnrunnable(code int) bool { return code == 127 || code == 126 }

// refusedCommandsIn predicts, WITHOUT running anything, which commands in cmd the read-only preamble
// would refuse. The runtime refusal is correct but arrives too late to help: it is reported as a
// transient progress line, which no model ever reads, and the agent's own shell is unwrapped — so it
// runs the very same command successfully, sees nothing wrong, and never substitutes. The check then
// yields no verdict for the whole run: not a false failure, but a silently missing gate. Predicting the
// refusal at AUTHORING time is what lets the check be repaired while repairing it is still cheap.
//
// It mirrors readOnlyPreamble deliberately, including its limits: names are matched as the shell
// resolves them (first word of each command position, path stripped), dual-use commands on their first
// ARGUMENT exactly as the `case "${1:-}"` does, so `git -C d commit` is not flagged — because it is not
// blocked either. Quoting is approximated: a blocked name inside a quoted string can be missed. Missing
// one costs a re-ask that would not have helped; claiming one that does not happen would send the review
// chasing a check that runs fine, so the bias is deliberately toward silence.
func refusedCommandsIn(cmd string) []string {
	if strings.TrimSpace(cmd) == "" {
		return nil
	}
	always := make(map[string]bool, len(blockedAlways))
	for _, c := range blockedAlways {
		if shellFuncName(c) { // the unnameable ones (g++, c++) are not shadowed, so not refused
			always[c] = true
		}
	}
	var out []string
	seen := make(map[string]bool)
	add := func(name string) {
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	for _, seg := range shellCommandSegments(cmd) {
		fields := strings.Fields(seg)
		// Step over what precedes the command word: env assignments (NAME=value) and the wrappers that
		// take a command as their argument. `command X` reaches the real binary by design — the preamble
		// uses it itself — so it is a skip, not a bypass to flag.
		i := 0
		for i < len(fields) && (isEnvAssignment(fields[i]) || fields[i] == "!" || fields[i] == "time" ||
			fields[i] == "exec" || fields[i] == "command" || fields[i] == "nohup" || fields[i] == "sudo") {
			i++
		}
		if i >= len(fields) {
			continue
		}
		name := shellWord(fields[i])
		if s := strings.LastIndexByte(name, '/'); s >= 0 {
			name = name[s+1:] // a path invocation is NOT shadowed, but flag it: /usr/bin/make still builds
		}
		args := fields[i+1:]
		switch {
		case always[name]:
			add(name)
		case name == "tar" && tarWrites(args):
			add("tar (create/extract)")
		default:
			subs, dual := blockedSubcommands[name]
			if !dual || len(args) == 0 {
				continue
			}
			for _, s := range subs {
				if args[0] == s {
					add(name + " " + s)
					break
				}
			}
		}
	}
	return out
}

// shellCommandSegments splits cmd at the operators that start a new command position. Every separator
// is treated alike because the question here is only "does a command word sit here", not what the
// control flow means. Substitutions are split on too, so `$(make x)` exposes its inner command.
// The split must be QUOTE-AWARE: a metacharacter inside a quoted argument is data, not a command
// boundary. A read-only `grep -q 'build\|make world\|x' f` splits, on a quote-blind scan, into a
// segment beginning `make` and is predicted refused — a false positive that costs a needless re-ask
// and, worse, marks a perfectly good check as refused in the worker's brief (observed live).
func shellCommandSegments(cmd string) []string {
	var segs []string
	var cur strings.Builder
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			segs = append(segs, s)
		}
		cur.Reset()
	}
	var quote byte // 0, '\'' or '"'
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		switch {
		case quote == '\'':
			if c == '\'' {
				quote = 0
			}
			cur.WriteByte(c)
		case quote == '"':
			if c == '\\' && i+1 < len(cmd) {
				cur.WriteByte(c)
				i++
				cur.WriteByte(cmd[i])
				continue
			}
			// Double quotes do NOT suspend command substitution: `"$(make -s x)"` really runs make,
			// so a substitution's boundaries still split. Single quotes suspend it, and are handled
			// above as pure data.
			if c == '`' || c == ')' || (c == '$' && i+1 < len(cmd) && cmd[i+1] == '(') {
				if c == '$' {
					i++
				}
				flush()
				continue
			}
			if c == '"' {
				quote = 0
			}
			cur.WriteByte(c)
		case c == '\'' || c == '"':
			quote = c
			cur.WriteByte(c)
		case c == '\\' && i+1 < len(cmd):
			// An escaped metacharacter is data too (`grep -q a\|b`), so carry both bytes across.
			cur.WriteByte(c)
			i++
			cur.WriteByte(cmd[i])
		case c == '|' || c == '&' || c == ';' || c == '\n' || c == '(' || c == ')' || c == '{' || c == '}' || c == '`':
			flush()
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return segs
}

// isEnvAssignment reports whether f is a leading NAME=value prefix rather than the command word.
func isEnvAssignment(f string) bool {
	i := strings.IndexByte(f, '=')
	if i <= 0 {
		return false
	}
	return shellFuncName(f[:i]) // same identifier rule the shell applies to a variable name
}

// shellWord strips a wrapping quote pair from a command word. Only a FULLY wrapped word is unquoted:
// a fragment left over from splitting inside a quoted string (`rm"`) must stay unrecognized, or a
// blocked name mentioned in a string argument would be reported as an invocation.
func shellWord(f string) string {
	if len(f) >= 2 && (f[0] == '"' || f[0] == '\'') && f[len(f)-1] == f[0] {
		return f[1 : len(f)-1]
	}
	return f
}

// tarWrites mirrors the preamble's tar case: create and extract write, list and everything else read.
func tarWrites(args []string) bool {
	if len(args) > 0 && (strings.HasPrefix(args[0], "c") || strings.HasPrefix(args[0], "x")) {
		return true // old-style bundled flags without a dash
	}
	joined := " " + strings.Join(args, " ")
	for _, f := range []string{" -c", " --create", " -x", " --extract"} {
		if strings.Contains(joined, f) {
			return true
		}
	}
	return false
}

// blockedCommandIn returns the command a refusal named, or "" when the output carries no refusal.
// Used to report WHICH command was blocked: the exit code says only that something was.
func blockedCommandIn(out string) string {
	i := strings.Index(out, readOnlyBlockMarker)
	if i < 0 {
		return ""
	}
	rest := out[i+len(readOnlyBlockMarker):]
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		rest = rest[:nl]
	}
	return strings.TrimSpace(rest)
}
