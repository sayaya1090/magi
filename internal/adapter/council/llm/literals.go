package llm

import (
	"fmt"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
)

// A task that names an identifier has stated a fact the work can be measured against, and the
// council was never handed that measurement.
//
// Measured (kv-store-grpc, a false done): the task says a SetValRequest "includes a key (string)
// and a value (int)", and — two lines away — that both responses carry a val (int). The agent
// wrote `val` in the request too, normalising to the identifier it had just typed twice. Its own
// end-to-end test passed, because it called its own stubs. Three members read the task and the
// work and voted done at full confidence.
//
// Nothing was hidden from them. The task was in front of them, the proto was in front of them, and
// the comparison is one a reader makes only if they think to make it. So magi makes it: the
// identifiers the task NAMES, and whether each one appears in what the turn wrote.
//
// This is a measurement, not a demand. It does not say a missing identifier is a defect — a task
// can name a thing the work legitimately calls something else, and the member is the one who
// decides. It also cannot reach for anything magi was not given: both sides are the task text and
// the agent's own files.

// literalsMax bounds how many identifiers are reported. Past a handful the list stops being read.
const literalsMax = 12

// codeLike matches the shapes a task uses for an identifier the work must contain: CamelCase
// (SetValRequest), snake_case (kv_store_pb2), a dotted filename (server.py), and a bare lowercase
// word ONLY when the task put it in a code position — see literalsInTask.
var (
	camelCase = regexp.MustCompile(`\b[A-Z][a-z0-9]+(?:[A-Z][a-zA-Z0-9]*)+\b`)
	snakeCase = regexp.MustCompile(`\b[a-z][a-z0-9]*(?:_[a-z0-9]+)+\b`)
	// A filename must START with a letter or digit. The fragment in "{class name}_pb2.py" is a
	// placeholder, not a name, and taking it made the measurement fire on a CORRECT run — which
	// is the one thing this must never do.
	dottedFile = regexp.MustCompile(`\b[A-Za-z0-9][A-Za-z0-9_.-]*\.(?:go|py|js|ts|c|h|cpp|rs|java|rb|sh|proto|json|ya?ml|toml|txt|dat|cbl)\b`)
	// namedField is the shape a task uses to specify one: "a value (int)", "a key (string)".
	// The parenthesised type is what separates a specification from prose.
	namedField = regexp.MustCompile(`\b([a-z][a-zA-Z0-9_]*)\s*\((?:int|int32|int64|string|bool|float|double|number)\d*\)`)
	backticked = regexp.MustCompile("`([^`\n]{1,60})`")
)

// literalsEnabled gates the section. Default on; MAGI_COUNCIL_LITERALS=0 restores the prior
// evidence for an A/B.
func literalsEnabled() bool {
	v := strings.TrimSpace(os.Getenv("MAGI_COUNCIL_LITERALS"))
	return v != "0" && !strings.EqualFold(v, "false") && !strings.EqualFold(v, "off")
}

// literalsInTask pulls the identifiers a task names. Conservative on purpose: a false entry here
// sends a member hunting for a phantom, which is the churn this council has been burned by, so
// only shapes that are unmistakably code are taken.
func literalsInTask(task string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if len(s) < 3 || len(s) > 60 || seen[s] {
			return
		}
		// A candidate that can never match is worse than no candidate: it reports a correct run as
		// missing something. Swept across eleven real tasks, three shapes do exactly that — a
		// placeholder (`/app/pmars-<version>/`, `{class name}_pb2.py`), a quoted escape
		// (`"\x03"`), and a signature fragment (`HeadlessTerminal(BaseTerminal)`). None of them
		// is an identifier the work can contain verbatim.
		if strings.ContainsAny(s, "<>{}()\"' \\") || strings.HasSuffix(s, "/") {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	for _, m := range dottedFile.FindAllString(task, -1) {
		add(m)
	}
	for _, m := range camelCase.FindAllString(task, -1) {
		add(m)
	}
	for _, m := range snakeCase.FindAllString(task, -1) {
		add(m)
	}
	for _, m := range namedField.FindAllStringSubmatch(task, -1) {
		add(m[1])
	}
	for _, m := range backticked.FindAllStringSubmatch(task, -1) {
		if f := strings.Fields(m[1]); len(f) == 1 {
			add(f[0]) // a backticked phrase is prose; a backticked token may be an identifier
		}
	}
	// A token that appears ONLY inside a quoted span is an example, not a name. The task that
	// showed this writes `call setreg('a', "your_keystrokes_here")` and expects the solution to
	// replace it — reporting its absence would accuse a correct run. A task that specifies an
	// identifier names it in prose (`a value (int)`, `a class called Server`).
	kept := out[:0]
	for _, lit := range out {
		if !onlyInsideQuotes(task, lit) {
			kept = append(kept, lit)
		}
	}
	out = kept
	sort.Strings(out)
	return out
}

// quotedSpanRE matches the quoted stretches of a task: what is inside them is illustration.
var quotedSpanRE = regexp.MustCompile(`"[^"\n]*"|'[^'\n]*'`)

// onlyInsideQuotes reports whether every occurrence of lit in task sits inside a quoted span.
func onlyInsideQuotes(task, lit string) bool {
	spans := quotedSpanRE.FindAllStringIndex(task, -1)
	inside := func(i int) bool {
		for _, sp := range spans {
			if i >= sp[0] && i < sp[1] {
				return true
			}
		}
		return false
	}
	found := false
	for i := 0; ; {
		j := strings.Index(task[i:], lit)
		if j < 0 {
			break
		}
		j += i
		found = true
		if !inside(j) {
			return false
		}
		i = j + 1
	}
	return found
}

// missingLiterals returns the task's identifiers that do not appear, as whole words, anywhere in
// the work. Whole words matter: "value" inside "values" is not the field being asked for, and
// reporting it as present is the way this measurement would quietly stop working.
func missingLiterals(task, work string) []string {
	if strings.TrimSpace(work) == "" {
		return nil // nothing was written; absence says nothing about identifiers
	}
	var out []string
	for _, lit := range literalsInTask(task) {
		if !presentInWork(work, lit) {
			out = append(out, lit)
		}
	}
	if len(out) > literalsMax {
		out = out[:literalsMax]
	}
	return out
}

// presentInWork reports whether the work carries this identifier, allowing for the ONE way the two
// sides legitimately spell the same thing differently: a task names a file by its absolute path
// (`/app/run.py`) and the evidence names it relative to the workdir (`### run.py (current content,
// full)`). Compared literally, such a candidate can never match — which is the failure this file's
// own rule forbids, "a candidate that can never match is worse than no candidate: it reports a
// correct run as missing something".
//
// Measured across 138 recorded council rounds: 46 fired, and 32 of those items were this — a file
// the agent had written, reported to the members as absent. `/app/run.py` alone accounted for 19,
// on a task whose very first tool call wrote it.
//
// Only the last segment is relaxed, and only for a candidate that has a directory part. A bare
// name stays exact, so `value` still cannot be satisfied by `values`.
func presentInWork(work, lit string) bool {
	if containsWord(work, lit) {
		return true
	}
	if base := path.Base(lit); base != lit && base != "." && base != "/" {
		return containsWord(work, base)
	}
	return false
}

// containsWord reports whether s contains lit bounded by non-identifier characters on both sides.
func containsWord(s, lit string) bool {
	for i := 0; ; {
		j := strings.Index(s[i:], lit)
		if j < 0 {
			return false
		}
		j += i
		before := j == 0 || !isIdentByte(s[j-1])
		end := j + len(lit)
		after := end == len(s) || !isIdentByte(s[end])
		if before && after {
			return true
		}
		i = j + 1
	}
}

func isIdentByte(b byte) bool {
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// literalsSection renders the measurement for the evidence block, or "" when there is nothing to
// report. The wording states what was compared and stops there: whether a missing identifier
// matters is the member's call, and a section that told them the answer would be magi voting.
func literalsSection(task, work string) string {
	missing := missingLiterals(task, work)
	if len(missing) == 0 {
		return ""
	}
	return fmt.Sprintf("The task names these identifiers, and none of them appears in what this turn "+
		"wrote: %s.\nThis is a comparison of two things you already have — the task's words and the "+
		"agent's own files — not a requirement magi is adding. An identifier can be absent for a good "+
		"reason; whether one of these is a defect is yours to judge.", strings.Join(missing, ", "))
}
