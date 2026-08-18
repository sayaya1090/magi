// Package jsonx parses JSON that a language model produced. Such a reply is not a document from a
// well-behaved encoder: it arrives wrapped in prose, carries stray braces from reasoning, is cut off
// by an output budget, and routinely embeds a raw newline inside a string that holds multi-line
// prose or a shell command. Rejecting it for any one of those discards content that was otherwise
// complete, so every reader of model output needs the same tolerance — and needs it to behave
// identically, which is why this lives in one place instead of being re-derived per call site.
package jsonx

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// BalancedObjects returns every TOP-LEVEL balanced {...} object in s, in order, respecting
// strings and escapes (braces inside string values don't confuse it). Nested objects (a
// plan's step objects) stay inside their parent — only depth-0 spans are returned — so the
// caller can try each candidate independently and skip a stray brace that precedes the real
// object.
func BalancedObjects(s string) []string { return balancedSpans(s, '{', '}') }

// BalancedArrays is BalancedObjects for [...] arrays — every TOP-LEVEL balanced array in s, in
// order, respecting strings and escapes. A JSON-array reply (e.g. a check-audit's list) that is
// wrapped in prose or trailed by reasoning containing a stray ] is recovered by trying each
// candidate, instead of a naive first-[/last-] span that mis-captures on any bracket outside the
// real array.
func BalancedArrays(s string) []string { return balancedSpans(s, '[', ']') }

// Objects is BalancedObjects plus the candidates a DAMAGED reply still yields, and it is what a
// reader of model output should call. The distinction matters because the two defects seen most
// often in practice both destroy the SPAN, not just the parse: a reply cut off by an output budget
// never closes its brace, and a stray quote after a closed container swallows the rest of the text —
// in both cases BalancedObjects returns an empty list, so the tolerant parse behind it never runs on
// anything. Repaired candidates come last, so a clean reply is still read from its own first span.
func Objects(s string) []string { return spansWithRecovery(s, '{', '}') }

func spansWithRecovery(s string, open, close byte) []string {
	var out []string
	seen := map[string]bool{}
	add := func(c string) {
		if c == "" || seen[c] {
			return
		}
		seen[c] = true
		out = append(out, c)
	}
	for _, c := range balancedSpans(s, open, close) {
		add(c)
	}
	// Each of these removes a token that only ever appears in a document json.Unmarshal was going to
	// reject, and each of them — not just the parse — destroys the SPAN: the extractor either runs off
	// the end or stops early, so without this the tolerant parse behind it is handed nothing to read.
	cleaned := DropDanglingPair(DropUnmatchedClosers(DropStrayQuoteAfterContainer(s)))
	if cleaned != s {
		for _, c := range balancedSpans(cleaned, open, close) {
			add(c)
		}
	}
	for _, src := range []string{s, cleaned} {
		// An EMPTY recovery carries nothing, and offering it would be the worst outcome of all: the
		// caller stops reporting that it could not read the reply and silently proceeds as if the model
		// had answered with nothing to say. Prose that merely contains a stray bracket recovers to `[]`.
		if c, ok := CloseTruncated(src); ok && c[0] == open && strings.TrimSpace(c[1:len(c)-1]) != "" {
			add(c)
		}
	}
	return out
}

// balancedSpans returns every TOP-LEVEL balanced open..close span in s, in order, respecting string
// literals and escapes so a bracket inside a quoted value never shifts the boundary. An open that
// never closes (a stray bracket in prose/reasoning — weak models emit these: a code fragment, a set
// literal, an unclosed example) is skipped so a real balanced span that follows it is still found,
// rather than letting one unclosed stray swallow the rest (observed: a multi-KB reply parsed to
// nothing). It backs both BalancedObjects and BalancedArrays.
func balancedSpans(s string, open, close byte) []string {
	var out []string
	i := 0
	for i < len(s) {
		start := strings.IndexByte(s[i:], open)
		if start < 0 {
			break
		}
		start += i
		depth, inStr, esc, end := 0, false, false, -1
		for j := start; j < len(s); j++ {
			ch := s[j]
			switch {
			case esc:
				esc = false
			case ch == '\\' && inStr:
				esc = true
			case ch == '"':
				inStr = !inStr
			case inStr:
				// inside a string literal — ignore structural chars
			case ch == open:
				depth++
			case ch == close:
				depth--
				if depth == 0 {
					end = j
				}
			}
			if end >= 0 {
				break
			}
		}
		if end < 0 {
			i = start + 1 // an unclosed stray; skip just it and keep scanning
			continue
		}
		out = append(out, s[start:end+1])
		i = end + 1
	}
	return out
}

// BalancedArrays is BalancedObjects for [...] arrays — every TOP-LEVEL balanced array in s, in
// order, respecting strings and escapes. A JSON-array reply (e.g. a check-audit's list) that is
// wrapped in prose or trailed by reasoning containing a stray ] is recovered by trying each
// candidate, instead of a naive first-[/last-] span that mis-captures on any bracket outside the
// balancedSpans returns every TOP-LEVEL balanced open..close span in s, in order, respecting string
// literals and escapes so a bracket inside a quoted value never shifts the boundary. An open that
// never closes (a stray bracket in prose/reasoning — weak models emit these: a code fragment, a set
// literal, an unclosed example) is skipped so a real balanced span that follows it is still found,
// rather than letting one unclosed stray swallow the rest (observed: a multi-KB reply parsed to
// balancedSpans returns every TOP-LEVEL balanced open..close span in s, in order, respecting string
// literals and escapes so a bracket inside a quoted value never shifts the boundary. An open that
// never closes (a stray bracket in prose/reasoning — weak models emit these: a code fragment, a set
// literal, an unclosed example) is skipped so a real balanced span that follows it is still found,
// rather than letting one unclosed stray swallow the rest (observed: a multi-KB reply parsed to
// StripTrailingCommas removes a comma that immediately precedes a closing } or ] (ignoring
// intervening whitespace). It respects string literals — a comma inside a quoted value is untouched —
// so it only repairs the structural trailing comma JSON forbids but weak models routinely emit.
func StripTrailingCommas(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inStr, esc := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			b.WriteByte(c)
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			b.WriteByte(c)
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
				j++
			}
			if j < len(s) && (s[j] == '}' || s[j] == ']') {
				continue // drop the trailing comma
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

// DropStrayQuoteAfterContainer removes a `"` that stands where JSON permits no value to begin: on
// the heels of a closed ] or } (whitespace skipped), where only a comma, another closer, or the end
// of the document is legal. Such a quote is not merely unparseable, it is CONTAGIOUS — the scanner
// enters a string literal and swallows the rest of the reply, so the closing brace never registers
// and the extractor returns no candidate at all. Observed live: a verdict ending `…failures."]" }`,
// where the whole vote was lost to one character. A legal document never reaches this shape, so the
// repair is identity on anything that was going to parse.
func DropStrayQuoteAfterContainer(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inStr, esc := false, false
	prev := byte(0) // last non-space byte written, outside strings
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			b.WriteByte(c)
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
				prev = c
			}
			continue
		}
		if c == '"' {
			if prev == ']' || prev == '}' {
				continue // stray: drop it and stay outside the string
			}
			inStr = true
			b.WriteByte(c)
			prev = c
			continue
		}
		b.WriteByte(c)
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			prev = c
		}
	}
	return b.String()
}

// DropUnmatchedClosers removes a } or ] that closes nothing — one that arrives with no container
// open, or that does not match the container it would close. Observed live: a check list ending
// `…"expect":"passed"}} ]`, where the element's own brace was written twice. Like a stray quote this
// costs the whole reply rather than the one element, because the extra closer drives the extractor's
// depth to zero early and the span it returns stops mid-document. A well-formed document has no such
// token, so this is identity on anything that was going to parse.
func DropUnmatchedClosers(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	var want []byte // the closer each open container is waiting for
	inStr, esc := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			b.WriteByte(c)
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			if len(want) > 0 { // a quote outside every container is prose
				inStr = true
			}
			b.WriteByte(c)
		case '{':
			want = append(want, '}')
			b.WriteByte(c)
		case '[':
			want = append(want, ']')
			b.WriteByte(c)
		case '}', ']':
			if len(want) == 0 || want[len(want)-1] != c {
				continue // closes nothing: drop it
			}
			want = want[:len(want)-1]
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// DropDanglingPair removes a trailing fragment inside an object that is not a key:value pair at all —
// everything from the last comma up to the closing brace, when no colon was written after that comma.
// Observed live: a check ending `…"command":"make all && make opt", ""}`, where a bare "" stands where
// a pair belongs. An object's final element always carries a colon of its own, so a document that was
// going to parse is returned unchanged; only the fragment that made it unreadable is dropped, and the
// pairs written before it survive.
func DropDanglingPair(s string) string {
	type frame struct {
		obj        bool
		lastComma  int
		colonSince bool
	}
	var stack []frame
	type span struct{ from, to int }
	var cuts []span
	inStr, esc := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			esc = false
		case inStr && c == '\\':
			esc = true
		case c == '"':
			if inStr || len(stack) > 0 {
				inStr = !inStr
			}
		case inStr:
		case c == '{' || c == '[':
			stack = append(stack, frame{obj: c == '{', lastComma: -1})
		case c == '}' || c == ']':
			if len(stack) == 0 {
				continue
			}
			f := stack[len(stack)-1]
			if f.obj && c == '}' && f.lastComma >= 0 && !f.colonSince {
				cuts = append(cuts, span{f.lastComma, i})
			}
			stack = stack[:len(stack)-1]
		case c == ',':
			if len(stack) > 0 {
				stack[len(stack)-1].lastComma = i
				stack[len(stack)-1].colonSince = false
			}
		case c == ':':
			if len(stack) > 0 {
				stack[len(stack)-1].colonSince = true
			}
		}
	}
	if len(cuts) == 0 {
		return s
	}
	// Objects close innermost-first, so the cuts arrive out of order and an outer one can contain an
	// inner one; ordering them lets the writer below skip what it has already dropped.
	sort.Slice(cuts, func(i, j int) bool { return cuts[i].from < cuts[j].from })
	var b strings.Builder
	b.Grow(len(s))
	prev := 0
	for _, cut := range cuts {
		if cut.from < prev { // a cut inside an already-dropped span
			continue
		}
		b.WriteString(s[prev:cut.from])
		prev = cut.to
	}
	b.WriteString(s[prev:])
	return b.String()
}

// CloseTruncated completes a document the model stopped writing partway through, and reports whether
// there was anything to complete. A reply cut off by an output budget is the single most costly shape
// here, because the extractor skips an opening brace that never closes: the caller is handed NOTHING,
// so a verdict whose decision was the very first field is recorded as an abstain, and a contract draft
// that carried nine of ten criteria is treated as no draft at all.
//
// The guarantee is that everything which arrived COMPLETE is kept and the fragment after it is not:
// the tail is cut back to the last comma of the innermost container — the signal that what precedes
// it finished — and the open containers are then closed. So a truncated criteria list yields the
// criteria that fully arrived, and a plan cut mid-step yields that step's completed fields and drops
// the half-written one it was in the middle of.
func CloseTruncated(s string) (string, bool) {
	type frame struct {
		close    byte
		lastGood int // index just past the last complete element in this container
	}
	var stack []frame
	start := -1
	inStr, esc := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			esc = false
		case inStr && c == '\\':
			esc = true
		case c == '"':
			// A quote outside every container is prose around the JSON, not a string delimiter.
			if inStr || len(stack) > 0 {
				inStr = !inStr
			}
		case inStr:
		case c == '{' || c == '[':
			cl := byte('}')
			if c == '[' {
				cl = ']'
			}
			if len(stack) == 0 {
				start = i
			}
			stack = append(stack, frame{close: cl, lastGood: i + 1})
		case c == '}' || c == ']':
			if len(stack) == 0 {
				continue // a stray closer in the prose
			}
			stack = stack[:len(stack)-1]
			if len(stack) > 0 {
				stack[len(stack)-1].lastGood = i + 1
			}
		case c == ',':
			if len(stack) > 0 {
				stack[len(stack)-1].lastGood = i // cut BEFORE the comma
			}
		}
	}
	if len(stack) == 0 || start < 0 {
		return "", false // nothing was left open: the document is whole, or there is no JSON in it
	}
	var b strings.Builder
	b.WriteString(s[start:stack[len(stack)-1].lastGood])
	for i := len(stack) - 1; i >= 0; i-- {
		b.WriteByte(stack[i].close)
	}
	return b.String(), true
}

// SalvagePrefix keeps the fields that arrived BEFORE a document's syntax error and reports whether
// anything was left to keep. It exists because a model's structural slip is almost never uniform
// across the reply: the defect lands in ONE container — an array left unclosed before the next key,
// a string that swallowed a brace — while everything ahead of it is well-formed. Rejecting the whole
// document then charges the caller for fields that arrived intact, and the first field is usually
// the one that mattered most (a verdict's `decision`, a report's `status`).
//
// It is deliberately NOT part of RepairCandidates or Unmarshal, for the same reason CloseTruncated
// is wired only into the span extractor: this recovery is LOSSY. Everything after the defect is
// discarded, so a plan cut in its third step would parse "successfully" as a two-step plan and lose
// the rest silently. Only a caller that has weighed that loss against its own alternative — for a
// council member, an abstain the tally cannot tell from "no opinion" — should reach for it, and it
// must say in its log that it did.
//
// The repairs are tried first, so a raw newline mid-prose is FIXED rather than treated as the cut
// point, and the longest surviving prefix across them wins. The result is re-parsed before it is
// returned, and a salvage that recovered no field at all is reported as nothing to salvage.
func SalvagePrefix(js string) (string, bool) {
	best := ""
	for _, c := range RepairCandidates(strings.TrimSpace(js)) {
		var probe any
		err := json.Unmarshal([]byte(c), &probe)
		if err == nil {
			return "", false // whole: the caller's failure was the schema, and a prefix cannot help that
		}
		var se *json.SyntaxError
		if !errors.As(err, &se) {
			continue
		}
		off := int(se.Offset)
		if off <= 0 || off > len(c) {
			continue
		}
		cut, ok := CloseTruncated(c[:off])
		if !ok || len(cut) < 2 || strings.TrimSpace(cut[1:len(cut)-1]) == "" {
			continue // nothing but the closers survived
		}
		if json.Unmarshal([]byte(cut), &probe) != nil {
			continue
		}
		if len(cut) > len(best) {
			best = cut
		}
	}
	return best, best != ""
}

// EscapeControlCharsInStrings rewrites raw control characters (< 0x20) that appear INSIDE a JSON
// string literal into their valid JSON escape (a literal newline/tab a weak model puts inside a
// "reason"/"task" value becomes \n/\t), which json.Unmarshal otherwise rejects with "invalid
// character ... in string literal" and StripTrailingCommas does not touch. Control characters
// OUTSIDE strings — the whitespace between tokens — are left exactly as they are, so only the illegal
// in-string ones are repaired. Respects escapes, so an already-escaped sequence is never doubled.
func EscapeControlCharsInStrings(s string) string {
	if strings.IndexFunc(s, func(r rune) bool { return r < 0x20 }) < 0 {
		return s // no control chars anywhere → nothing to repair
	}
	var b strings.Builder
	b.Grow(len(s) + 16)
	inStr, esc := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !inStr {
			if c == '"' {
				inStr = true
			}
			b.WriteByte(c)
			continue
		}
		switch {
		case esc:
			esc = false
			b.WriteByte(c)
		case c == '\\':
			esc = true
			b.WriteByte(c)
		case c == '"':
			inStr = false
			b.WriteByte(c)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\t':
			b.WriteString(`\t`)
		case c == '\r':
			b.WriteString(`\r`)
		case c < 0x20:
			fmt.Fprintf(&b, `\u%04x`, c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// EscapeInvalidEscapes rewrites a backslash that does not begin a legal JSON escape into a literal
// one (\\), inside string literals only.
//
// JSON allows exactly \" \\ \/ \b \f \n \r \t and \uXXXX. Anything else is a parse error at that
// byte, and the two shapes that produce it are ordinary content a model has no reason to think
// twice about: a Windows-style path and a regex. Observed live in a council reply —
//
//	"cite":"custom-memory-heap-crash__VwdqwmF\exception.txt — 80.6 KB"
//
// where `\e` killed the document at offset 1106 of 1332 and the salvage path kept only the prefix,
// so the member's decision survived and its feedback and grounds did not. `\d+` in a feedback
// string does the same thing.
//
// The backslash is DOUBLED rather than dropped: the model wrote a path separator or a regex, and
// both mean a literal backslash. A bad \u (not four hex digits) is treated the same way — the
// alternative is inventing a code point.
func EscapeInvalidEscapes(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	isHex := func(c byte) bool {
		return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
	}
	var b strings.Builder
	b.Grow(len(s) + 16)
	inStr := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !inStr {
			if c == '"' {
				inStr = true
			}
			b.WriteByte(c)
			continue
		}
		if c == '"' {
			inStr = false
			b.WriteByte(c)
			continue
		}
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		if i+1 >= len(s) { // a string that ends on a backslash: it can only be a literal one
			b.WriteString(`\\`)
			continue
		}
		switch n := s[i+1]; {
		case strings.IndexByte(`"\\/bfnrt`, n) >= 0:
			b.WriteByte(c)
			b.WriteByte(n)
			i++
		case n == 'u' && i+5 < len(s) && isHex(s[i+2]) && isHex(s[i+3]) && isHex(s[i+4]) && isHex(s[i+5]):
			b.WriteString(s[i : i+6])
			i += 5
		default:
			b.WriteString(`\\`) // not an escape the format has — the backslash was literal
		}
	}
	return b.String()
}

// RepairCandidates returns js followed by the weak-model repair variants a failed unmarshal
// should be retried with: a trailing comma before }/] , a RAW control character (literal
// newline/tab) inside a string value, and both together. Each is an error json.Unmarshal rejects
// outright but that a weak model routinely emits — a multi-line "reason" or "task" string is the
// common source of the control char. Candidates are de-duplicated and ordered cheapest-first, so a
// clean object still parses on the first try.
//
// It is shared by every lenient JSON reader (the plan object AND the salvaged step objects): a
// repair the clean path applies but the salvage path does not is worse than useless, since the
// truncation that forces salvage is produced by exactly the rambling that emits these defects.
func RepairCandidates(js string) []string {
	out := []string{js}
	seen := map[string]bool{js: true}
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	// Light repairs first: a trailing comma and a raw control character are the common defects and
	// the cheapest to undo.
	light := EscapeInvalidEscapes(EscapeControlCharsInStrings(StripTrailingCommas(js)))
	add(StripTrailingCommas(js))
	add(EscapeControlCharsInStrings(js))
	add(EscapeInvalidEscapes(js))
	add(light)
	// Structural repairs on top: an unescaped inner quote, a single-quoted string and a bare
	// identifier value are ALL already-invalid JSON, so these can only act on a document that was
	// going to be rejected — but they rewrite more than whitespace, so they come after the light
	// ones and the caller always tries the original first.
	add(EscapeStrayQuotes(js))
	add(EscapeStrayQuotes(light))
	add(DropStrayQuoteAfterContainer(light))
	add(DropDanglingPair(DropUnmatchedClosers(DropStrayQuoteAfterContainer(light))))
	quoted := SingleToDoubleQuotes(light)
	add(quoted)
	add(QuoteBareValues(quoted))
	add(QuoteBareValues(light))
	return out
}

// Unmarshal parses js into v, retrying with the shared weak-model repairs before failing.
// Every reader of model-produced JSON needs it for the same reason: the payloads carry multi-line
// prose (a reason, a task, a criterion) or shell commands, so an unescaped control character is the
// normal shape of the data rather than an edge case — and rejecting the document over one discards
// content that was otherwise complete.
func Unmarshal(js string, v any) bool {
	for _, c := range RepairCandidates(js) {
		if json.Unmarshal([]byte(c), v) == nil {
			return true
		}
	}
	return false
}

// Text is a free-text field that also accepts the shapes a model emits when it ignores the schema's
// type: a LIST (it enumerates instead of writing prose — joined, since each element is part of what
// the field says) or a NUMBER. Go's decoder aborts the whole document on the first type mismatch,
// so without this one such field costs every sibling field and every element beside it.
type Text string

func (v *Text) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*v = Text(s)
		return nil
	}
	var list []string
	if json.Unmarshal(b, &list) == nil {
		*v = Text(strings.Join(list, "; "))
		return nil
	}
	var n json.Number
	if json.Unmarshal(b, &n) == nil {
		*v = Text(n.String())
		return nil
	}
	*v = ""
	return nil
}

// Number is a numeric field that also accepts a QUOTED number ("0.9"), which a model routinely
// emits where the schema says a float. A strict float64 rejected the whole reply over it — for a
// council verdict that means the member is recorded as abstaining and a vote that was cast is lost.
type Number float64

func (v *Number) UnmarshalJSON(b []byte) error {
	var f float64
	if json.Unmarshal(b, &f) == nil {
		*v = Number(f)
		return nil
	}
	var s string
	if json.Unmarshal(b, &s) == nil {
		if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
			*v = Number(f)
			return nil
		}
	}
	*v = 0
	return nil
}

// SingleToDoubleQuotes rewrites Python-style '...' strings into JSON "..." strings. A quote
// character outside a string is ALREADY invalid JSON, so this can only ever act on a document that
// was going to be rejected anyway — it cannot corrupt a well-formed one. Observed live: a planner
// reply that opened with double quotes and switched to single quotes partway through its array,
// which cost the entire plan.
func SingleToDoubleQuotes(s string) string {
	if !strings.ContainsRune(s, '\'') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	inDouble, inSingle, esc := false, false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			esc = false
			b.WriteByte(c)
		case c == '\\':
			esc = true
			b.WriteByte(c)
		case inDouble:
			if c == '"' {
				inDouble = false
			}
			b.WriteByte(c)
		case inSingle:
			switch c {
			case '\'':
				inSingle = false
				b.WriteByte('"')
			case '"':
				b.WriteString(`\"`) // a double quote inside the converted string must be escaped
			default:
				b.WriteByte(c)
			}
		case c == '"':
			inDouble = true
			b.WriteByte(c)
		case c == '\'':
			inSingle = true
			b.WriteByte('"')
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// EscapeStrayQuotes escapes a double quote that appears INSIDE a string value without being
// escaped. In valid JSON the quote that closes a string is always followed — after whitespace — by
// one of `,` `}` `]` `:` or the end of the document, so a quote followed by anything else cannot be
// a terminator and must have been meant as literal text. Escaping it recovers the value the model
// meant to write; on a well-formed document every quote passes the lookahead test, so this is the
// identity.
//
// Observed live: a council member quoting a command inside its own criterion —
// `"criteria":["",""make -C x" passes without failure."]` — which cost the member's ENTIRE verdict
// (recorded as an abstain) and skewed the tally. Quoting a command or an identifier inside a prose
// field is exactly what these fields invite, so this is a structural defect of the data rather than
// an edge case.
func EscapeStrayQuotes(s string) string {
	if !strings.Contains(s, `"`) {
		return s
	}
	terminates := func(i int) bool { // is the quote at i the one that closes the string?
		for j := i + 1; j < len(s); j++ {
			switch s[j] {
			case ' ', '\t', '\n', '\r':
				continue
			case ',', '}', ']', ':':
				return true
			default:
				return false
			}
		}
		return true // end of document
	}
	var b strings.Builder
	b.Grow(len(s) + 16)
	inStr, esc := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !inStr {
			if c == '"' {
				inStr = true
			}
			b.WriteByte(c)
			continue
		}
		switch {
		case esc:
			esc = false
			b.WriteByte(c)
		case c == '\\':
			esc = true
			b.WriteByte(c)
		case c == '"':
			if terminates(i) {
				inStr = false
				b.WriteByte(c)
			} else {
				b.WriteString(`\"`)
			}
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// bareValueRe matches a colon followed by a BARE identifier value — `"agent":explore` — up to the
// next structural character. JSON has exactly four unquoted values (true, false, null, a number),
// so anything else there is already a syntax error; the alternation below excludes those four so a
// legal document is never touched.
var bareValueRe = regexp.MustCompile(`(:\s*)([A-Za-z_][A-Za-z0-9_./-]*)(\s*[,}\]])`)

// QuoteBareValues quotes an unquoted string value. Like SingleToDoubleQuotes it only fires on text
// that is already invalid JSON. Observed live: `{"agent":explore,"focus":"caml/"}` in a planner
// reply — one bare token discarding the whole plan.
func QuoteBareValues(s string) string {
	if !strings.Contains(s, ":") {
		return s
	}
	return bareValueRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := bareValueRe.FindStringSubmatch(m)
		switch strings.ToLower(sub[2]) {
		case "true", "false", "null": // legal bare values — leave them alone
			return m
		}
		return sub[1] + `"` + sub[2] + `"` + sub[3]
	})
}

// Diagnose names WHY a model reply could not be read, in one line. An excerpt alone is not enough:
// it keeps the head and the tail, so a defect in the middle of a long reply — the common case, since
// the middle is where the multi-line prose fields live — is exactly what it hides. Observed: a
// 905-byte verdict whose head and tail were both well-formed.
//
// It reports one of three outcomes, which is the distinction the reader actually needs:
//   - no JSON at all (the model answered in prose)
//   - a SYNTAX defect: Go's own message, the byte offset, and a window around it, so the defect
//     class is named without guessing (an invalid escape and an unescaped quote read alike in an
//     excerpt)
//   - it PARSES: the failure was the schema, not the syntax — so the keys it did carry are listed,
//     which is what tells a renamed field from a wrong type
//
// Diagnose is for logs only; it never changes what a caller parses.
func Diagnose(js string) string {
	span := strings.TrimSpace(js)
	if !strings.HasPrefix(span, "{") && !strings.HasPrefix(span, "[") {
		// The JSON is embedded in prose: diagnose the largest balanced span, which is the one a
		// lenient reader would have tried.
		cands := append(BalancedObjects(js), BalancedArrays(js)...)
		span = ""
		for _, c := range cands {
			if len(c) > len(span) {
				span = c
			}
		}
		if span == "" {
			return "no JSON object or array in the reply (the model answered in prose)"
		}
	}
	var probe any
	err := json.Unmarshal([]byte(span), &probe)
	if err == nil {
		if m, ok := probe.(map[string]any); ok {
			keys := make([]string, 0, len(m))
			for k := range m {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			return "the JSON parses — the mismatch is the SCHEMA, not the syntax; keys: [" + strings.Join(keys, " ") + "]"
		}
		return "the JSON parses — the mismatch is the SCHEMA, not the syntax"
	}
	off := -1
	var se *json.SyntaxError
	var ute *json.UnmarshalTypeError
	switch {
	case errors.As(err, &se):
		off = int(se.Offset)
	case errors.As(err, &ute):
		off = int(ute.Offset)
	}
	if off < 0 || off > len(span) {
		return "syntax error: " + err.Error()
	}
	// Offsets point just PAST the offending byte, so bias the window to keep it in view.
	const w = 70
	lo, hi := off-w, off+w
	if lo < 0 {
		lo = 0
	}
	if hi > len(span) {
		hi = len(span)
	}
	around := strings.Join(strings.Fields(span[lo:off]), " ") + " ⟪HERE⟫ " + strings.Join(strings.Fields(span[off:hi]), " ")
	return fmt.Sprintf("syntax error at offset %d of %d (%v): …%s…", off, len(span), err, around)
}

// Report renders a FAILED parse the one way every call site should render it: the bounded excerpt
// (what the model said) followed by the named reason (why it could not be read). Either half alone
// leaves the failure untraceable — the excerpt hides a defect in the middle, and the reason alone
// loses the content — and two sites rendering the same failure differently look like two failures.
func Report(text string) string { return Excerpt(text) + "  ⟨" + Diagnose(text) + "⟩" }

// Excerpt renders a model reply as one bounded line, keeping the HEAD and the TAIL. Both ends
// matter when diagnosing: the head shows what shape the model chose, the tail shows whether it was
// cut off. Whitespace is collapsed so the excerpt stays on one log line. Callers differ in where
// they report it — an event, stderr — but the rendering must not, or two reports of the same
// failure look like two different failures.
func Excerpt(s string) string {
	t := strings.Join(strings.Fields(s), " ")
	const n = 200
	if len(t) <= 2*n {
		return t
	}
	return fmt.Sprintf("%s …[%d chars omitted]… %s", t[:n], len(t)-2*n, t[len(t)-n:])
}
