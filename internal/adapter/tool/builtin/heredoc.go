package builtin

import "strings"

// A heredoc body is data the shell hands a program verbatim. Everything that reads a command as
// shell text — the detach scan here, the redirect scan in the app layer — has to skip those bodies,
// and both used to do it with their own copy of a one-line `strings.Index(line, "<<")`. The copies
// drifted the way copies do: they were fixed once for the case that had been observed live and kept
// every case that had not. This is the one implementation both use.
//
// Measured against the copies it replaces, on inputs that reach either scan:
//
//	cat > a <<'A' && cat > b <<'B'   B's body was scanned as shell text (only the first `<<` was
//	                                 found), so a `&` in it read as a detach and a `>` as a redirect
//	cat > f.c <<'EOF' … "  EOF"      an INDENTED terminator ended the body early for a plain `<<`,
//	                                 which bash would not do, and the rest of the C was shell text
//	echo "shift << left"             a quoted `<<` opened a heredoc that never terminated, so every
//	# see notes << here              following line was swallowed and a real `&` further down was
//	                                 never seen
//	grep x <<<word                   `<<<` is a here-STRING: no body, no terminator. Read as a
//	                                 heredoc on `word`, it swallowed the rest of the command.

// heredocIntro is one heredoc opener: the word that ends its body, and whether the `<<-` form was
// used — that form, and only that form, lets the terminator be indented with tabs.
type heredocIntro struct {
	delim  string
	dashed bool
}

// terminates reports whether line is this heredoc's terminator. bash wants the delimiter alone on
// the line; `<<-` additionally strips leading TABS (not spaces). A trailing CR is tolerated because
// it comes from the file's line endings, not from the shell.
func (h heredocIntro) terminates(line string) bool {
	line = strings.TrimRight(line, "\r")
	if h.dashed {
		line = strings.TrimLeft(line, "\t")
	}
	return line == h.delim
}

// heredocDelimWord returns the delimiter a `<<` introduces, given the text immediately after the
// operator, or "" when what follows is not a delimiter word — an arithmetic left-shift (`$((1<<2))`)
// being the case that matters. Quoting the word (`<<'EOF'`) is how a body is kept from expanding
// and does not change the word itself.
func heredocDelimWord(afterLtLt string) string {
	s := strings.TrimLeft(afterLtLt, "-")
	s = strings.TrimLeft(s, " \t")
	f := strings.Fields(s)
	if len(f) == 0 {
		return ""
	}
	word := strings.Trim(f[0], "'\"")
	if word == "" {
		return ""
	}
	if c := word[0]; c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
		return word
	}
	return ""
}

// scanShellLine walks one line of shell text once, reporting the first detaching `&` in it and
// every heredoc it opens, in the order the shell will read their bodies.
//
// One pass, because the two answers depend on the same quote state and two passes would each
// mutate it. quote carries in and out so a quoted run opened on one line and closed on another is
// still one region.
func scanShellLine(line string, quote *byte) (detachAt int, intros []heredocIntro) {
	detachAt = -1
	for i := 0; i < len(line); i++ {
		c := line[i]
		if *quote != 0 {
			// Inside double quotes a backslash escapes the next byte; inside single quotes
			// nothing is special.
			if *quote == '"' && c == '\\' && i+1 < len(line) {
				i++
				continue
			}
			if c == *quote {
				*quote = 0
			}
			continue
		}
		switch {
		case c == '\'' || c == '"':
			*quote = c
		case c == '#' && (i == 0 || isCommentStart(line[i-1])):
			// The rest of the line is a comment: no operator in it is one.
			return detachAt, intros
		case c == '<' && i+1 < len(line) && line[i+1] == '<':
			if i+2 < len(line) && line[i+2] == '<' {
				i += 2 // a here-string has no body to skip
				continue
			}
			rest := line[i+2:]
			d := heredocDelimWord(rest)
			if d == "" {
				i++ // an arithmetic left-shift, not an intro
				continue
			}
			intros = append(intros, heredocIntro{delim: d, dashed: strings.HasPrefix(rest, "-")})
			i++ // the operator's second `<`; the delimiter word is ordinary text from here
		case c == '&' && detachAt < 0:
			if i+1 < len(line) && line[i+1] == '&' {
				i++ // list operator, not a detach
				continue
			}
			// `2>&1` / `>&2` / `<&0`: the & names a file descriptor, not a job.
			j := i - 1
			for j >= 0 && (line[j] == ' ' || line[j] == '\t') {
				j--
			}
			if j >= 0 && (line[j] == '>' || line[j] == '<') {
				continue
			}
			detachAt = i
		}
	}
	return detachAt, intros
}

// isCommentStart reports whether a `#` preceded by b begins a comment. A `#` in the middle of a
// word (`file#1`, `${x#y}`) does not.
func isCommentStart(b byte) bool {
	switch b {
	case ' ', '\t', ';', '|', '&', '(':
		return true
	}
	return false
}

// StripHeredocBodies removes every heredoc body, and the terminator line that closes it, leaving
// the lines of a command that are actually shell text. A command with no heredoc comes back
// unchanged.
func StripHeredocBodies(cmd string) string {
	if !strings.Contains(cmd, "<<") {
		return cmd
	}
	lines := strings.Split(cmd, "\n")
	out := lines[:0:0]
	var quote byte
	for i := 0; i < len(lines); i++ {
		out = append(out, lines[i])
		_, intros := scanShellLine(lines[i], &quote)
		for _, h := range intros {
			for i+1 < len(lines) && !h.terminates(lines[i+1]) {
				i++
			}
			if i+1 < len(lines) {
				i++ // and the terminator itself
			}
		}
	}
	return strings.Join(out, "\n")
}

// HasHeredoc reports whether cmd opens a real heredoc — the thing that makes a command an author of
// file content — as opposed to carrying an arithmetic left-shift, a here-string, or a `<<` inside
// quotes or a comment.
func HasHeredoc(cmd string) bool {
	if !strings.Contains(cmd, "<<") {
		return false
	}
	var quote byte
	for _, line := range strings.Split(cmd, "\n") {
		if _, intros := scanShellLine(line, &quote); len(intros) > 0 {
			return true
		}
	}
	return false
}
