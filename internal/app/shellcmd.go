package app

import "strings"

// Shell-command classification helpers for the run guard: a heuristic (quoting-agnostic)
// tokenizer that decides whether a bash command only INSPECTS state or actually EXECUTES /
// writes something, plus heredoc and redirect parsing. Pure functions over the command
// string, split out of guard.go; they hold no runGuard state.

// inspectOnlyCmds are shell builtins/coreutils whose job is to PRINT or INSPECT state,
// never to run a program-under-test. A bash command built only from these cannot verify a
// deliverable — it can restate that a file exists or echo a success banner, the exact
// "looks-verified" churn a fabricating agent emits (measured on Terminal-Bench: an agent
// ran `ls`/`echo`/`exit 0`/`true` dozens of times without once executing its own module).
// The set is deliberately small and CLOSED — POSIX inspection verbs — the opposite of an
// open-ended confession-phrase list. Anything not here (python, pytest, go, ./run, make, a
// script) counts as execution, so the bias is toward NOT flagging; this is only an advisory.
var inspectOnlyCmds = map[string]bool{
	"true": true, "false": true, ":": true, "exit": true, "echo": true, "printf": true,
	"ls": true, "pwd": true, "cd": true, "cat": true, "head": true, "tail": true,
	"wc": true, "stat": true, "file": true, "which": true, "type": true, "test": true,
	"[": true, "[[": true, "sleep": true, "clear": true, "dirname": true, "basename": true,
	"realpath": true, "readlink": true, "tee": true, // tee authors content, it does not run a program
	// Filters and reporters: they read their input and print. None can run a program-under-test,
	// and every one of them is what an agent reaches for to LOOK at a file — which is exactly the
	// churn this classification exists to see through. Observed live: one run printed the same
	// eighty lines through `sed -n`, `cat | sed`, and `cat -n | sed`, and the record credited each
	// as a command that had exercised something and ended clean.
	"sed": true, "grep": true, "egrep": true, "fgrep": true, "rgrep": true,
	"sort": true, "uniq": true, "cut": true, "tr": true, "nl": true, "rev": true, "column": true,
	"od": true, "xxd": true, "hexdump": true, "strings": true, "cmp": true, "diff": true,
	"date": true, "uname": true, "id": true, "whoami": true, "hostname": true,
	"du": true, "df": true, "less": true, "more": true,
	// Deliberately NOT here: awk (can system()), find (-exec), env (env FOO=1 cmd runs cmd),
	// xargs, git (log inspects, checkout mutates). The bias stays toward calling it execution.
}

// sedInPlace reports whether a sed/perl argument list carries the in-place flag — `-i`, or `-i.bak`
// / `-i'.bak'` with a backup suffix glued on. sed prints its input until `-i`, at which point it
// rewrites the file: the same binary on both sides of the line this classification draws, so the
// verb alone cannot answer. Written once and read from all three places that ask (the inspect-only
// classification, the mutation predicate, and the write-path extractor), because three copies of a
// flag test drift and none of them can fail a build when they do.
func sedInPlace(args []string) bool {
	for _, f := range args {
		if f == "-i" || f == "--in-place" ||
			strings.HasPrefix(f, "-i.") || strings.HasPrefix(f, "-i'") ||
			strings.HasPrefix(f, "--in-place=") {
			return true
		}
	}
	return false
}

// isInspectOnly reports whether EVERY segment of cmd (split on the shell operators &&, ||,
// ;, |, &, and newlines) is inspect-only — i.e. the whole command runs nothing that could
// exercise a deliverable. A segment is execution if its first token is PATH-QUALIFIED
// (contains `/`, e.g. `./test`, `/usr/bin/foo`, `bin/run`) — a path always runs a program,
// never a shell inspection builtin, so this is checked before the builtin-name lookup and
// keeps a binary that happens to be named `test`/`sleep` from reading as the builtin.
// Otherwise the bare name is looked up in inspectOnlyCmds, and a verb that some flag turns into a
// writer (`sed -i`) is execution despite the name. This is a heuristic tokenizer that
// does not honor quoting, which is fine: nothing it feeds asserts anything to the model.
// An empty command counts as inspect-only (it ran nothing).
func isInspectOnly(cmd string) bool {
	segs := splitShellSegments(stripHeredocs(cmd))
	if len(segs) == 0 {
		return true
	}
	for _, s := range segs {
		fields := strings.Fields(s)
		if len(fields) == 0 {
			continue // empty segment (e.g. a trailing operator) — nothing ran here
		}
		tok := fields[0]
		if isRedirectFragment(tok) {
			continue // a split fd-dup/redirect artifact ("1" from `2>&1`, ">f" from `&>f`), not a command
		}
		if strings.ContainsRune(tok, '/') {
			return false // a path-qualified command runs a program
		}
		if !inspectOnlyCmds[tok] {
			return false
		}
		if (tok == "sed" || tok == "perl") && sedInPlace(fields[1:]) {
			return false // -i rewrites the file; without it the same command only prints
		}
	}
	return true
}

// isRedirectFragment reports whether tok is a leftover piece of a redirect, not a command
// name — because splitShellSegments cuts on `&`, an fd-duplication like `2>&1` or `>&2` is
// torn into a segment whose first token is the redirect tail (`1`, `2`) or a redirect operator
// (`>file` from `&>file`). Such a segment ran no program, so it must not read as execution.
// A real command never begins with a digit or a redirect operator.
func isRedirectFragment(tok string) bool {
	if tok == "" {
		return true
	}
	switch tok[0] {
	case '>', '<', '&':
		return true
	}
	for i := 0; i < len(tok); i++ {
		if tok[i] < '0' || tok[i] > '9' {
			return false
		}
	}
	return true // all digits (e.g. the `1` in `2>&1`)
}

// splitShellSegments breaks a command line into its pipeline/list segments on the shell
// control operators, so each can be classified independently ("ls && python x" is NOT
// inspect-only). Two-char operators are recognized before their one-char prefixes.
//
// QUOTES ARE HONORED, because an operator inside them is data, not a separator. Observed live:
// `grep -r "run.*length\|rle\|POOL_FREE_HEADER" --include="*.c" | head -100` was split at the
// alternations INSIDE the pattern, leaving a fragment whose first token was `rle\` — a name no
// inspect verb matches, so a plain grep was classified as a program run and reported to the
// council as a command that had exercised the deliverable and ended clean. Any regex alternation,
// any `find … -o …` predicate, any path with a semicolon does the same.
func splitShellSegments(cmd string) []string {
	var out []string
	seg := strings.Builder{}
	var quote byte // 0, '\'' or '"'
	flush := func() {
		if strings.TrimSpace(seg.String()) != "" {
			out = append(out, seg.String())
		}
		seg.Reset()
	}
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		if quote != 0 {
			// Inside single quotes nothing is special. Inside double quotes a backslash escapes
			// the next byte, so an escaped quote does not close the run.
			if quote == '"' && c == '\\' && i+1 < len(cmd) {
				seg.WriteByte(c)
				i++
				seg.WriteByte(cmd[i])
				continue
			}
			if c == quote {
				quote = 0
			}
			seg.WriteByte(c)
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
			seg.WriteByte(c)
		case '&', '|':
			if i+1 < len(cmd) && cmd[i+1] == c { // && or ||
				i++
			}
			flush()
		case ';', '\n':
			flush()
		default:
			seg.WriteByte(c)
		}
	}
	flush()
	return out
}

// leadingVerb returns the basename of a segment's first token, or "" if the segment is
// blank. Env-assignment or wrapper prefixes (FOO=bar, sudo, env) are left as-is, which
// classifies them as execution — the FP-safe direction, since such prefixes front real
// commands far more often than inspect-only ones.
func leadingVerb(seg string) string {
	fields := strings.Fields(seg)
	if len(fields) == 0 {
		return ""
	}
	v := fields[0]
	if i := strings.LastIndexByte(v, '/'); i >= 0 {
		v = v[i+1:]
	}
	return v
}

// fileMutateVerbs are commands whose SUCCESS mutates the filesystem by their very purpose,
// no redirect involved: file coreutils, patchers, archive extractors, sync tools. They are
// the redirect-less counterpart of redirectsToFile for the mutation epoch bump — without
// them, a bash-heavy fix cycle (`sed -i`, `patch`, `cp`) registers NO progress, so the
// build/test commands re-run between fixes read as identical no-progress repeats and the
// loop guard blocks the third build even though the tree genuinely changed between runs.
// The set is deliberately small and CLOSED, like inspectOnlyCmds. Build/test runners
// (make, go build, pytest) are deliberately NOT here: they are execution evidence
// (noteBashExec), and their artifacts are derived state — counting them as mutations would
// let a pure rebuild spin reset its own repeat counter forever.
var fileMutateVerbs = map[string]bool{
	"cp": true, "mv": true, "rm": true, "mkdir": true, "rmdir": true, "touch": true,
	"ln": true, "install": true, "truncate": true, "chmod": true, "chown": true,
	"patch": true, "rsync": true, "dd": true,
	"unzip": true, "gunzip": true, "bunzip2": true, "unxz": true, "zstd": true,
}

// mutatingSubcommands maps a tool to the subcommands that mutate the worktree or the
// environment (as opposed to its read-only queries: `git status`, `go vet`, `npm ls`).
var mutatingSubcommands = map[string]map[string]bool{
	"git": {"apply": true, "checkout": true, "restore": true, "reset": true, "merge": true,
		"rebase": true, "cherry-pick": true, "revert": true, "stash": true, "mv": true,
		"rm": true, "clean": true, "pull": true, "clone": true, "am": true, "commit": true},
	"go":   {"mod": true, "get": true, "generate": true},
	"pip":  {"install": true, "uninstall": true},
	"pip3": {"install": true, "uninstall": true},
	"npm":  {"install": true, "ci": true, "i": true, "add": true, "uninstall": true, "remove": true},
	"yarn": {"add": true, "install": true, "remove": true},
	"pnpm": {"install": true, "add": true, "remove": true},
	"apt":  {"install": true, "remove": true}, "apt-get": {"install": true, "remove": true},
	"apk": {"add": true, "del": true}, "yum": {"install": true, "remove": true},
	"dnf": {"install": true, "remove": true}, "brew": {"install": true, "uninstall": true},
}

// queryingSubcommands are the read-only forms of a subcommand that otherwise mutates: the third
// token turns the command back into a question. `git stash` stashes the worktree; `git stash list`
// prints what is stashed and writes nothing.
//
// The distinction is not cosmetic. mutatesFiles feeds the epoch bump, and a bump RESTARTS the
// no-progress window — so a pure query counted as a mutation hands a circling agent a fresh window
// for asking a question. Observed live: an agent looking for edits it could not find ran
// `git stash list 2>&1 || echo "not a git repo"` in a container with no `.git`; the command changed
// nothing, could change nothing, and reset the counter anyway.
var queryingSubcommands = map[string]map[string]bool{
	"git": {"stash list": true, "stash show": true},
}

// mutatesFiles reports whether any segment of cmd runs a redirect-less file-mutating
// command: a fileMutateVerbs coreutil, an in-place `sed -i`/`perl -i`, a mutating
// tool subcommand (git apply, go mod, pip install, …), or a tar with a
// create/extract/update flag. Same heuristic tokenizer family as isInspectOnly —
// quoting-agnostic, feeding only the advisory epoch bump.
func mutatesFiles(cmd string) bool {
	for _, seg := range splitShellSegments(stripHeredocs(cmd)) {
		fields := strings.Fields(seg)
		if len(fields) == 0 {
			continue
		}
		verb := leadingVerb(seg)
		if fileMutateVerbs[verb] {
			return true
		}
		switch verb {
		case "sed", "perl":
			if sedInPlace(fields[1:]) {
				return true
			}
		case "tar":
			if len(fields) > 1 {
				flags := strings.TrimPrefix(fields[1], "-")
				if strings.ContainsAny(flags, "xcru") && !strings.Contains(flags, "t") {
					return true
				}
			}
		default:
			subs := mutatingSubcommands[verb]
			if subs == nil || len(fields) < 2 || !subs[fields[1]] {
				continue
			}
			// …unless a third token names the read-only form of that subcommand.
			if len(fields) > 2 && queryingSubcommands[verb][fields[1]+" "+fields[2]] {
				continue
			}
			return true
		}
	}
	return false
}

// redirectsToFile reports whether cmd sends output into a real file — a heredoc (`<<`), a
// pipe into `tee`, or a `>`/`>>` whose target is an actual path. It deliberately EXCLUDES
// file-descriptor duplications (`2>&1`, `>&2`, `>&-`) and `/dev/*` sinks (`>/dev/null`),
// which capture or discard a running command's output rather than author a deliverable — so
// that running tests with `pytest 2>&1` or `./run >/dev/null` is not mistaken for producing
// a new deliverable version. Heuristic; does not honor quoting, which is fine for the
// advisory epoch bump it feeds.
func redirectsToFile(cmd string) bool {
	if hasHeredoc(cmd) { // heredoc authors content
		return true
	}
	for _, seg := range splitShellSegments(cmd) {
		if leadingVerb(seg) == "tee" {
			return true
		}
	}
	return len(redirectTargets(cmd)) > 0
}

// redirectTargets returns the file paths a command's `>`/`>>` operators write to, in order,
// applying redirectsToFile's exclusions (fd duplications, /dev sinks, a bare `>`).
func redirectTargets(cmd string) []string {
	var out []string
	for i := 0; i < len(cmd); i++ {
		if cmd[i] != '>' {
			continue
		}
		j := i
		for j < len(cmd) && cmd[j] == '>' { // consume a `>>` run as one redirect
			j++
		}
		i = j - 1 // resume scanning after this redirect operator
		k := j
		for k < len(cmd) && (cmd[k] == ' ' || cmd[k] == '\t') {
			k++
		}
		if k < len(cmd) && cmd[k] == '&' {
			continue // fd duplication (`>&1`, `>&-`) — not a file
		}
		end := k
		for end < len(cmd) && !isRedirectStop(cmd[end]) {
			end++
		}
		target := cmd[k:end]
		if target == "" || strings.HasPrefix(target, "/dev/") {
			continue // bare `>` with no visible target, or a /dev sink
		}
		out = append(out, target)
	}
	return out
}

// bashWriteCap bounds how many destinations one bash call contributes, so a wide `rm`/`tee`
// cannot flood the per-turn content history the self-revert check keeps.
const bashWriteCap = 8

// dropRedirects removes a segment's redirection tokens so what remains is the command's own
// operands. Without this a redirect is counted as an argument, and both ways that lands wrong:
// `cp a.bak a.c 2>/dev/null` reads as THREE operands and is skipped as a directory copy (the
// silenced restore is the commonest revert shape there is), while `rm f 2>/dev/null` adopts
// `2>/dev/null` itself as a written file. The destinations are not lost — redirectTargets reads
// them straight off the command text.
func dropRedirects(fields []string) []string {
	out := fields[:0:0]
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if !strings.ContainsAny(f, "<>") {
			out = append(out, f)
			continue
		}
		// `> f` / `2>> f`: the operator stands alone and its target is the next token.
		if strings.HasSuffix(f, ">") || strings.HasSuffix(f, "<") {
			i++
		}
	}
	return out
}

// bashWritePaths names the files a mutating bash command writes, so a bash mutation can go
// through the SAME content-level self-revert check (noteEdit) that write/edit already do.
// It is deliberately NARROWER than redirectsToFile/mutatesFiles, which only have to answer
// "did this author something": here a wrong path compares the wrong file's content, so a
// destination that is not unambiguous from the command text yields nothing and the command
// keeps its epoch bump with no revert check. Skipped for that reason: globs and anything
// needing shell expansion, directory targets, multi-source `cp`/`mv`, recursive copies, and
// `patch`/`git apply`/archive extraction (their destinations live inside the payload).
// Missing a revert is the safe direction — inventing one would retract real progress.
func bashWritePaths(cmd string) []string {
	var out []string
	add := func(p string) {
		p = strings.Trim(p, `"'`)
		if p == "" || len(out) >= bashWriteCap {
			return
		}
		if strings.HasPrefix(p, "/dev/") || strings.HasSuffix(p, "/") {
			return // a sink, or a directory target whose real destination we cannot name
		}
		if strings.ContainsAny(p, "*?[]{}$`~<>") {
			return // needs a shell to resolve, or is a redirect fragment: either reads the wrong file
		}
		for _, seen := range out {
			if seen == p {
				return
			}
		}
		out = append(out, p)
	}
	for _, t := range redirectTargets(cmd) {
		add(t)
	}
	for _, seg := range splitShellSegments(stripHeredocs(cmd)) {
		fields := dropRedirects(strings.Fields(seg))
		if len(fields) < 2 {
			continue
		}
		switch leadingVerb(seg) {
		case "tee":
			for _, f := range fields[1:] {
				if !strings.HasPrefix(f, "-") {
					add(f)
				}
			}
		case "sed", "perl":
			// Only `-i` rewrites in place; without it the command prints and changes nothing.
			// Operands are the trailing files — the first is the script UNLESS `-e`/`-f` supplied it.
			inPlace, scripted := sedInPlace(fields[1:]), false
			var operands []string
			for i := 1; i < len(fields); i++ {
				f := fields[i]
				if sedInPlace([]string{f}) {
					continue
				}
				if f == "-e" || f == "-f" {
					scripted, i = true, i+1 // the script is the NEXT token, not a file
					continue
				}
				if strings.HasPrefix(f, "-") {
					continue
				}
				operands = append(operands, f)
			}
			if !inPlace {
				continue
			}
			if !scripted && len(operands) > 0 {
				operands = operands[1:] // the leading operand was the script
			}
			for _, f := range operands {
				add(f)
			}
		case "cp", "mv":
			// Exactly one source and one destination, no recursion: anything else targets a
			// directory whose per-file destinations we would have to guess.
			var operands []string
			recursive := false
			for _, f := range fields[1:] {
				if strings.HasPrefix(f, "-") {
					if strings.ContainsAny(strings.TrimPrefix(f, "-"), "rRa") {
						recursive = true
					}
					continue
				}
				operands = append(operands, f)
			}
			if !recursive && len(operands) == 2 {
				add(operands[1])
			}
		case "rm", "touch":
			// A delete is a real content state (""), so create→rm→create is a revert like any other.
			for _, f := range fields[1:] {
				if !strings.HasPrefix(f, "-") {
					add(f)
				}
			}
		}
	}
	return out
}

// stripHeredocs removes heredoc bodies (and their terminator lines) so that content fed via
// `cmd <<TAG … TAG` is not mistaken for commands when we split on newlines to classify a bash
// call. It keeps the introducing line (which carries the real leading verb, e.g. `cat > f`).
func stripHeredocs(cmd string) string {
	if !strings.Contains(cmd, "<<") {
		return cmd
	}
	lines := strings.Split(cmd, "\n")
	out := lines[:0:0]
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		out = append(out, line)
		idx := strings.Index(line, "<<")
		if idx < 0 {
			continue
		}
		delim := heredocDelim(line[idx+2:])
		if delim == "" {
			continue // a `<<` that is not a heredoc intro (e.g. arithmetic left-shift)
		}
		for i+1 < len(lines) && strings.TrimSpace(lines[i+1]) != delim {
			i++ // drop body line
		}
		if i+1 < len(lines) {
			i++ // drop the terminator line
		}
	}
	return strings.Join(out, "\n")
}

// hasHeredoc reports whether cmd contains a real heredoc (`cmd <<TAG`), as opposed to an
// arithmetic left-shift (`$((1<<2))`) — distinguished by whether the token after `<<` is a
// valid delimiter word (see heredocDelim).
func hasHeredoc(cmd string) bool {
	for i := 0; i+1 < len(cmd); i++ {
		if cmd[i] == '<' && cmd[i+1] == '<' && heredocDelim(cmd[i+2:]) != "" {
			return true
		}
	}
	return false
}

// heredocDelim returns the delimiter word a `<<` introduces (given the text right after the
// `<<`), or "" if this is not a heredoc. Handles `<<-` and quoted `<<'EOF'`/`<<"EOF"`. A
// heredoc delimiter is an identifier — it must begin with a letter or underscore — so an
// arithmetic shift like `1<<2` (whose "delimiter" would start with a digit) returns "".
func heredocDelim(afterLtLt string) string {
	s := strings.TrimLeft(afterLtLt, "-<") // <<- and any run of extra <
	s = strings.TrimLeft(s, " \t")         // optional space before the word
	f := strings.Fields(s)
	if len(f) == 0 {
		return ""
	}
	word := strings.Trim(f[0], "'\"")
	if word == "" {
		return ""
	}
	c := word[0]
	if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
		return word
	}
	return ""
}

// isRedirectStop reports whether b ends a redirect target token.
func isRedirectStop(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '|', ';', '&', '<', '>':
		return true
	}
	return false
}
