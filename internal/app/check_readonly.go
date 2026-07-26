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
	b.WriteString("__magi_ro_block() { printf '%s%s\\n' '" + readOnlyBlockMarker + "' \"$1\" >&2; return 126; }\n")
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
	return readOnlyPreamble() + cmd
}

// checkUnrunnable reports whether an exit code means the CHECK itself could not run, as opposed to
// the deliverable failing. 127 is "command not found" (a missing tool or a wrong path). 126 is "found
// but not executable" — a permission error, and now also the read-only guard's refusal. Both say
// nothing about the artifact, so every gate skips them rather than reworking correct code; 126 used
// to be counted as a failure at each of those sites, which false-failed a deliverable whenever a
// check hit a non-executable file. subst_review already paired the two, and this makes the gates agree.
func checkUnrunnable(code int) bool { return code == 127 || code == 126 }

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
