package app

import "strings"

// Shell-command classification helpers for the run guard: a heuristic (quoting-agnostic)
// tokenizer that decides whether a bash command only INSPECTS state or actually EXECUTES /
// writes something, plus heredoc and redirect parsing. Pure functions over the command
// string, split out of guard.go; they feed the advisory unverifiedDeliverable signal and
// hold no runGuard state. Behavior unchanged.

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
}

// isInspectOnly reports whether EVERY segment of cmd (split on the shell operators &&, ||,
// ;, |, &, and newlines) is inspect-only — i.e. the whole command runs nothing that could
// exercise a deliverable. A segment is execution if its first token is PATH-QUALIFIED
// (contains `/`, e.g. `./test`, `/usr/bin/foo`, `bin/run`) — a path always runs a program,
// never a shell inspection builtin, so this is checked before the builtin-name lookup and
// keeps a binary that happens to be named `test`/`sleep` from reading as the builtin.
// Otherwise the bare name is looked up in inspectOnlyCmds. This is a heuristic tokenizer that
// does not honor quoting, which is fine: it only feeds the advisory unverifiedDeliverable
// signal. An empty command counts as inspect-only (it ran nothing).
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
	}
	return true
}

// isNoOpBanner reports whether cmd is a pure "completion banner": a command that only prints
// or trivially succeeds, writing to no file and running no program-under-test. It is exactly
// the keep-alive an agent spams every turn to dodge the len(toolCalls)==0 finish gate after
// declaring the task done. It is DELIBERATELY NARROWER than isInspectOnly: it excludes
// ls/cat/exit/false and rejects any redirect or pipe, because those either read real state or
// author/feed a file (a legitimate `cat`/`ls`/`grep` exploration or an `echo … > f` write must
// NOT read as a spin). The verb set is exactly {echo, printf, true, :} (a leading `cd` is
// tolerated as a no-op prefix). This set MUST stay in lockstep with the offline calibration
// classifier that measured the passing-run banner-streak ceiling (=8); widening it (e.g. adding
// exit/false) raised that ceiling to 11 in the data and would invalidate bannerSpinStop.
func isNoOpBanner(cmd string) bool {
	if strings.ContainsAny(cmd, ">|") || strings.Contains(cmd, "tee ") {
		return false // authors/feeds a file or pipes into another program — not a no-op
	}
	saw := false
	for _, seg := range strings.Split(strings.ReplaceAll(cmd, "&&", ";"), ";") {
		fields := strings.Fields(seg)
		if len(fields) == 0 {
			continue // empty segment (e.g. a trailing operator) — nothing ran here
		}
		switch fields[0] {
		case "cd":
			continue // a no-op prefix (`cd /app && echo …`); does not itself disqualify
		case "echo", "printf", "true", ":":
			saw = true
		default:
			return false
		}
	}
	return saw
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
// inspect-only). Two-char operators are replaced before their one-char prefixes.
func splitShellSegments(cmd string) []string {
	repl := cmd
	for _, op := range []string{"&&", "||", ";", "|", "\n", "&"} {
		repl = strings.ReplaceAll(repl, op, "\x00")
	}
	var out []string
	for _, p := range strings.Split(repl, "\x00") {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
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
		return true
	}
	return false
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
