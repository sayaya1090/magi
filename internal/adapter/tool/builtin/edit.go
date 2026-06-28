package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Edit replaces a string in a file. It matches exactly first; if that fails it
// retries tolerantly (line-ending and trailing-whitespace differences) because
// models routinely reproduce a snippet with slightly-off whitespace — a strict
// "not found" there wastes turns and fails edits. Indentation (leading
// whitespace) is NOT guessed: a near-miss returns a pointer instead, so we never
// silently mis-indent a replacement. (F-TOOL-EDIT)
type Edit struct{}

type editArgs struct {
	Path       string `json:"path"`
	Old        string `json:"old"`
	New        string `json:"new"`
	ReplaceAll bool   `json:"replaceAll"`
}

func (Edit) Name() string { return "edit" }
func (Edit) Description() string {
	return "Replace a string in a file (unique match unless replaceAll). Matching tolerates line-ending and trailing-whitespace differences; leading indentation must match."
}
func (Edit) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"old":{"type":"string"},"new":{"type":"string"},"replaceAll":{"type":"boolean"}},"required":["path","old","new"]}`)
}

func (Edit) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var a editArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	if a.Old == a.New {
		return errResult("", "no change: old equals new"), nil
	}
	abs, err := resolvePath(env.Workdir, a.Path)
	if err != nil {
		return errResult("", err.Error()), nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return errResult("", "file not found: "+a.Path), nil
		}
		return errResult("", err.Error()), nil
	}

	updated, note, eerr := applyEdit(string(data), a.Old, a.New, a.ReplaceAll)
	if eerr != nil {
		return errResult("", eerr.Error()), nil
	}
	if err := os.WriteFile(abs, []byte(updated), 0o644); err != nil {
		return errResult("", err.Error()), nil
	}
	// Report the 1-based start line of the edit so the UI can number the diff (only
	// for an exact match — tolerant matches omit it). Appended last as " @N".
	msg := "edited " + a.Path + note
	if idx := strings.Index(string(data), a.Old); idx >= 0 {
		msg += fmt.Sprintf(" @%d", 1+strings.Count(string(data)[:idx], "\n"))
	}
	return okText("", msg), nil
}

// applyEdit returns the updated content, a note about how the match was made, or
// an error. Strategy: exact → EOL-normalized exact → trailing-whitespace-tolerant
// whole-line match → helpful near-miss error.
func applyEdit(content, old, new string, all bool) (string, string, error) {
	// 1. Exact.
	if c := strings.Count(content, old); c > 0 {
		if c > 1 && !all {
			return "", "", fmt.Errorf("not unique: old string occurs %d times — add surrounding context to disambiguate, or set replaceAll", c)
		}
		if all {
			return strings.ReplaceAll(content, old, new), "", nil
		}
		return strings.Replace(content, old, new, 1), "", nil
	}

	// 2. EOL-normalized exact: make old/new use the file's dominant line ending.
	crlf := strings.Contains(content, "\r\n")
	oldN, newN := toEOL(old, crlf), toEOL(new, crlf)
	if oldN != old {
		if c := strings.Count(content, oldN); c > 0 {
			if c > 1 && !all {
				return "", "", fmt.Errorf("not unique: old string occurs %d times — add surrounding context, or set replaceAll", c)
			}
			if all {
				return strings.ReplaceAll(content, oldN, newN), " (matched ignoring line endings)", nil
			}
			return strings.Replace(content, oldN, newN, 1), " (matched ignoring line endings)", nil
		}
	}

	// 3. Trailing-whitespace-tolerant, whole-line match against the original
	// bytes (so the rest of the file — including its line endings — is untouched).
	if updated, n, err := replaceFlexible(content, old, new, all); err != nil {
		return "", "", err
	} else if n > 0 {
		return updated, " (matched ignoring trailing whitespace)", nil
	}

	// 4. Not found — point at the closest near-miss so the model can self-correct.
	return "", "", fmt.Errorf("not found: old string not present%s", nearMissHint(content, old))
}

// toEOL converts all line endings in s to CRLF (crlf=true) or LF.
func toEOL(s string, crlf bool) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if crlf {
		s = strings.ReplaceAll(s, "\n", "\r\n")
	}
	return s
}

// lineKey is the whitespace-tolerant comparison key for one line.
func lineKey(line string) string {
	return strings.TrimRight(strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), " \t")
}

// replaceFlexible finds the block of whole lines whose trailing-trimmed form
// equals old's, and splices in new. Returns the updated content and match count.
func replaceFlexible(content, old, new string, all bool) (string, int, error) {
	oldBody := strings.TrimSuffix(strings.ReplaceAll(old, "\r\n", "\n"), "\n")
	oldLines := strings.Split(oldBody, "\n")
	if len(oldLines) == 0 {
		return content, 0, nil
	}
	oldKeys := make([]string, len(oldLines))
	for i, l := range oldLines {
		oldKeys[i] = lineKey(l)
	}

	starts, lines := lineSpans(content) // starts[i]..starts[i+1] is line i (with terminator)
	var matches []int                   // starting line index of each match
	for i := 0; i+len(oldKeys) <= len(lines); i++ {
		ok := true
		for j := range oldKeys {
			if lineKey(lines[i+j]) != oldKeys[j] {
				ok = false
				break
			}
		}
		if ok {
			matches = append(matches, i)
		}
	}
	if len(matches) == 0 {
		return content, 0, nil
	}
	if len(matches) > 1 && !all {
		return "", 0, fmt.Errorf("not unique: %d whitespace-tolerant matches — add surrounding context, or set replaceAll", len(matches))
	}

	// Build replacement text using the matched region's terminator style.
	apply := func(start int) (s, e int, repl string) {
		s = starts[start]
		e = starts[start+len(oldKeys)]
		region := content[s:e]
		repl = new
		if strings.HasSuffix(region, "\r\n") {
			repl = toEOL(new, true)
			if !strings.HasSuffix(repl, "\r\n") {
				repl += "\r\n"
			}
		} else if strings.HasSuffix(region, "\n") && !strings.HasSuffix(repl, "\n") {
			repl += "\n"
		}
		return s, e, repl
	}

	var b strings.Builder
	prev := 0
	for _, m := range matches {
		s, e, repl := apply(m)
		b.WriteString(content[prev:s])
		b.WriteString(repl)
		prev = e
		if !all {
			break
		}
	}
	b.WriteString(content[prev:])
	return b.String(), len(matches), nil
}

// lineSpans returns the start offset of each line plus the line strings (each
// including its terminator); starts has len(lines)+1 entries (last = len).
func lineSpans(content string) (starts []int, lines []string) {
	starts = append(starts, 0)
	for i := 0; i < len(content); i++ {
		if content[i] == '\n' {
			lines = append(lines, content[starts[len(starts)-1]:i+1])
			starts = append(starts, i+1)
		}
	}
	if starts[len(starts)-1] < len(content) { // trailing line with no newline
		lines = append(lines, content[starts[len(starts)-1]:])
		starts = append(starts, len(content))
	} else if len(content) == 0 {
		lines = append(lines, "")
	}
	return starts, lines
}

// nearMissHint reports where old's first non-blank line appears, to help the
// model fix whitespace/indentation in a retry.
func nearMissHint(content, old string) string {
	var first string
	for _, l := range strings.Split(strings.ReplaceAll(old, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(l) != "" {
			first = strings.TrimSpace(l)
			break
		}
	}
	if first == "" {
		return ""
	}
	for i, l := range strings.Split(content, "\n") {
		if strings.Contains(l, first) {
			return fmt.Sprintf(" — but line %d contains %q; check leading indentation in 'old'", i+1, strings.TrimSpace(l))
		}
	}
	return ""
}
