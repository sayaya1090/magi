// Package jsonx parses JSON that a language model produced. Such a reply is not a document from a
// well-behaved encoder: it arrives wrapped in prose, carries stray braces from reasoning, is cut off
// by an output budget, and routinely embeds a raw newline inside a string that holds multi-line
// prose or a shell command. Rejecting it for any one of those discards content that was otherwise
// complete, so every reader of model output needs the same tolerance — and needs it to behave
// identically, which is why this lives in one place instead of being re-derived per call site.
package jsonx

import (
	"encoding/json"
	"fmt"
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
	for _, fixed := range []string{
		StripTrailingCommas(js),
		EscapeControlCharsInStrings(js),
		EscapeControlCharsInStrings(StripTrailingCommas(js)),
	} {
		if seen[fixed] {
			continue
		}
		seen[fixed] = true
		out = append(out, fixed)
	}
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

// Texts is a list-of-strings field that also accepts a SINGLE string (the model answers with one
// item instead of a one-element list) and drops elements it cannot render as text.
type Texts []string

func (v *Texts) UnmarshalJSON(b []byte) error {
	var list []Text
	if json.Unmarshal(b, &list) == nil {
		out := make([]string, 0, len(list))
		for _, t := range list {
			if s := strings.TrimSpace(string(t)); s != "" {
				out = append(out, s)
			}
		}
		*v = out
		return nil
	}
	var one Text
	if json.Unmarshal(b, &one) == nil && strings.TrimSpace(string(one)) != "" {
		*v = Texts{strings.TrimSpace(string(one))}
		return nil
	}
	*v = nil
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
