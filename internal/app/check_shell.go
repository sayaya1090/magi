package app

import "strings"

// The provenance audit reads shell commands the worker ALREADY ran, recorded by magi's own tool log,
// and asks where a file's content came from. That means walking a command string the way the shell
// does — finding each command position, seeing through env prefixes and quoting — without executing
// anything. These helpers are that reading, and nothing here decides a verdict on its own.
//
// They are deliberately approximate at the edges (a name inside a quoted string can be missed) because
// the audit only REPORTS: a miss costs a note that is not printed, never a wrong gate.

// shellFuncName reports whether name is a POSIX shell identifier. Used to tell a leading `NAME=value`
// env assignment from the command word — a word containing `+` or `-` (g++, apt-get) is never one.
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

// shellCommandSegments splits cmd at the operators that start a new command position. Every separator
// is treated alike because the question here is only "does a command word sit here", not what the
// control flow means. Substitutions are split on too, so `$(make x)` exposes its inner command.
// The split must be QUOTE-AWARE: a metacharacter inside a quoted argument is data, not a command
// boundary — `grep -q 'build\|make world\|x' f` must not be read as a segment beginning `make`.
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
