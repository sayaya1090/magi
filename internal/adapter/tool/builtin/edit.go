package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/text/unicode/norm"

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
	Path       string   `json:"path"`
	Old        string   `json:"old"`
	New        string   `json:"new"`
	At         string   `json:"at"`
	To         string   `json:"to"`
	ReplaceAll flexBool `json:"replaceAll"`
}

func (Edit) Name() string { return "edit" }
func (Edit) Description() string {
	return "Replace text in a file. Default: give `old` (a unique snippet) and `new`; matching tolerates line-ending and trailing-whitespace drift but leading indentation must match. Anchored: instead give `at` a line number \"N\" from a read (optionally `to` a second number for a range) and `new` as the replacement — it replaces those whole lines. Use `at` alone, not with `old`."
}
func (Edit) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"old":{"type":"string"},"new":{"type":"string"},"at":{"type":"string","description":"line number N from a read to anchor the edit; replaces line N (through ` + "`to`" + ` if given)"},"to":{"type":"string","description":"end line number N for a range replace with ` + "`at`" + `"},"replaceAll":{"type":"boolean"}},"required":["path"]}`)
}

func (Edit) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var a editArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	anchored := strings.TrimSpace(a.At) != ""
	if anchored {
		if a.Old != "" {
			return errResult("", "provide either `old` or `at`, not both — `at` anchors to a line ref and `new` replaces it"), nil
		}
	} else {
		// An empty old string matches at every character boundary, so the uniqueness
		// check would report a nonsensical "occurs N times". Reject it with a clear
		// message (use write to create/replace whole files, or `at` to anchor by line).
		if a.Old == "" {
			return errResult("", "old string must not be empty (use write to create or replace a whole file, or `at` to anchor by line ref)"), nil
		}
		if a.Old == a.New {
			return errResult("", "no change: old equals new"), nil
		}
	}
	abs, err := resolvePath(env.Workdir, a.Path)
	if err != nil {
		return errResult("", err.Error()), nil
	}
	// Serialize the whole read-modify-write against concurrent edits/writes of the
	// same file (parallel subagents sharing a workdir), so neither loses the other's
	// update. Per-path, so edits to different files stay parallel.
	defer pathLocks.lock(lockKey(abs))()
	data, err := os.ReadFile(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return errResult("", "file not found: "+a.Path), nil
		}
		return errResult("", err.Error()), nil
	}

	if anchored {
		return applyAnchoredEdit(string(data), a, abs), nil
	}

	updated, note, eerr := applyEdit(string(data), a.Old, a.New, bool(a.ReplaceAll))
	if eerr != nil {
		return errResult("", eerr.Error()), nil
	}
	if err := atomicWriteFile(abs, []byte(updated), 0o644); err != nil {
		return errResult("", err.Error()), nil
	}
	// Report the 1-based start line of the edit so the UI can number the diff (only
	// for an exact match — tolerant matches omit it). Appended last as " @N".
	msg := "edited " + a.Path + note
	if idx := strings.Index(string(data), a.Old); idx >= 0 {
		msg += fmt.Sprintf(" @%d", 1+strings.Count(string(data)[:idx], "\n"))
	}
	msg += commentNoiseAdvisory(a.New, a.Old)
	return okText("", msg), nil
}

// applyAnchoredEdit handles the `at`(/`to`) path: it validates the line number(s)
// against the CURRENT file (rejecting a past-end reference) and replaces the
// anchored line or range with a.New.
func applyAnchoredEdit(content string, a editArgs, abs string) session.ToolResult {
	from, err := checkAnchor(content, a.At)
	if err != nil {
		return errResult("", err.Error())
	}
	to := from
	if strings.TrimSpace(a.To) != "" {
		t, err := checkAnchor(content, a.To)
		if err != nil {
			return errResult("", err.Error())
		}
		if t < from {
			return errResult("", fmt.Sprintf("`to` line %d is before `at` line %d — order the anchors low→high", t, from))
		}
		to = t
	}
	updated := replaceLineRange(content, from, to, a.New)
	if updated == content {
		return errResult("", "no change: the anchored range already equals `new`")
	}
	if err := atomicWriteFile(abs, []byte(updated), 0o644); err != nil {
		return errResult("", err.Error())
	}
	span := fmt.Sprintf(" @%d", from)
	if to != from {
		span = fmt.Sprintf(" @%d-%d", from, to)
	}
	return okText("", "edited "+a.Path+span+commentNoiseAdvisory(a.New, content))
}

// notUniqueErr builds the ambiguous-match error. It names the line anchors of every
// occurrence (capped) so the model can pivot to the `at`/`to` anchored mode in ONE
// retry instead of guessing how much context to add — the dominant real-world edit
// failure is exactly this loop. The anchors replace WHOLE lines, so the advice says so.
func notUniqueErr(content, old string, count int, spans []anchorSpan) error {
	list := make([]string, 0, len(spans))
	for _, s := range spans {
		list = append(list, s.String())
	}
	return fmt.Errorf("not unique: old string occurs %d times, at %s — re-send with `at` (and `to` for a multi-line span) from ONE of these to replace those whole lines with `new`, or add surrounding context to `old`, or set replaceAll",
		count, strings.Join(list, ", "))
}

// anchorSpan is one occurrence's whole-line span, as `at`/`to` refs.
type anchorSpan struct{ at, to string }

func (s anchorSpan) String() string {
	if s.to == "" || s.to == s.at {
		return s.at
	}
	return s.at + ".." + s.to
}

// maxAnchorSpans caps how many occurrences an error enumerates.
const maxAnchorSpans = 8

// substringSpans locates each occurrence of old (exact substring) in content and
// returns its whole-line anchor span (start line ref, end line ref).
func substringSpans(content, old string) []anchorSpan {
	lines := fileLines(content)
	ref := func(n int) string { return strconv.Itoa(n) } // 1-based line → "N"
	span := 1 + strings.Count(strings.TrimSuffix(old, "\n"), "\n")
	var out []anchorSpan
	for idx, pos := 0, 0; len(out) < maxAnchorSpans; pos += idx + len(old) {
		idx = strings.Index(content[pos:], old)
		if idx < 0 {
			break
		}
		start := 1 + strings.Count(content[:pos+idx], "\n")
		end := start + span - 1
		if end > len(lines) {
			end = len(lines)
		}
		out = append(out, anchorSpan{at: ref(start), to: ref(end)})
	}
	return out
}

// applyEdit returns the updated content, a note about how the match was made, or
// an error. Strategy: exact → EOL-normalized exact → trailing-whitespace-tolerant
// whole-line match → helpful near-miss error.
func applyEdit(content, old, new string, all bool) (string, string, error) {
	// 1. Exact.
	if c := strings.Count(content, old); c > 0 {
		if c > 1 && !all {
			return "", "", notUniqueErr(content, old, c, substringSpans(content, old))
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
				return "", "", notUniqueErr(content, oldN, c, substringSpans(content, oldN))
			}
			if all {
				return strings.ReplaceAll(content, oldN, newN), " (matched ignoring line endings)", nil
			}
			return strings.Replace(content, oldN, newN, 1), " (matched ignoring line endings)", nil
		}
	}

	// 2.5 Unicode-normalization-tolerant exact. A file saved on macOS is frequently
	// stored NFD (decomposed Hangul/accents: "함" → ㅎ+ㅏ+ㅁ) while a model emits NFC
	// (precomposed) — visually identical but byte-unequal, so the exact tiers miss and
	// the edit fails with "not found". Try old in the file's likely form (NFC then NFD)
	// and, on a hit, write new in that SAME form so the rest of the file's normalization
	// is left untouched. crlf is reused from tier 2 so this composes with EOL differences.
	for _, form := range []norm.Form{norm.NFC, norm.NFD} {
		oldF := toEOL(form.String(old), crlf)
		if oldF == old {
			continue // this form is what we already tried exactly above
		}
		if c := strings.Count(content, oldF); c > 0 {
			if c > 1 && !all {
				return "", "", notUniqueErr(content, oldF, c, substringSpans(content, oldF))
			}
			newF := toEOL(form.String(new), crlf)
			if all {
				return strings.ReplaceAll(content, oldF, newF), " (matched ignoring unicode normalization)", nil
			}
			return strings.Replace(content, oldF, newF, 1), " (matched ignoring unicode normalization)", nil
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

// lineKey is the whitespace- and normalization-tolerant comparison key for one line:
// trailing EOL/whitespace stripped and Unicode folded to NFC, so an NFD file line and an
// NFC old line (the macOS Hangul case) compare equal even alongside whitespace drift.
func lineKey(line string) string {
	return norm.NFC.String(strings.TrimRight(strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), " \t"))
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
		spans := make([]anchorSpan, 0, min(len(matches), maxAnchorSpans))
		for _, m := range matches[:min(len(matches), maxAnchorSpans)] {
			start, end := m+1, m+len(oldKeys)
			spans = append(spans, anchorSpan{
				at: strconv.Itoa(start),
				to: strconv.Itoa(end),
			})
		}
		return "", 0, notUniqueErr(content, old, len(matches), spans)
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
