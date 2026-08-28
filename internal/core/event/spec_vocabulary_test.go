package event

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The event vocabulary is written down in five places and was consistent in none of them.
//
// Measured 2026-08-29, before this test existed: `tool.started` was named by all eight docs and had
// no constant and no emitter — DIAGRAMS drew the arrow in a sequence diagram. `artifact.emitted`
// (six docs) and `diagnostic` (ARCHITECTURE) named types the code does not have either. Going the
// other way, `labels.changed`, `result.elided`, `interjection.answered` and `question.answered`
// were in no document at all — the log's own readers were the ones left uninformed. SPEC's
// normative R2 listed four types, one of which did not exist; its Korean twin added `agent.*`,
// which is not even a prefix here.
//
// ARCHITECTURE showed how this survives review: it hand-listed ten transients and then said, in
// the very next sentence, that the set is enumerated once in `transientTypes` instead of being
// re-listed. Three of its ten disagreed with that map. A reader who believes that sentence has no
// reason to check, which is worse than a list that admits it is a copy.
//
// So SPEC keeps carrying the sets — an outside client needs the names, and a pointer into Go
// source is not a specification — but it no longer carries them on trust. This test is the
// F-EVENT-FACT-TRANSIENT `vocab-1` probe: the two rules must name exactly what the code declares,
// in both languages. Adding a type without touching SPEC fails here, which is the point; the
// failure names the missing or surplus type so the fix is mechanical.
func TestSpecStatesTheWholeEventVocabulary(t *testing.T) {
	facts, transients := declaredSets(t)
	for _, doc := range []string{"docs/SPEC.md", "docs/SPEC.ko.md"} {
		body := readRepoFile(t, doc)
		sec := section(t, doc, body, "### F-EVENT-FACT-TRANSIENT")
		compare(t, doc+" R1 (persisted)", facts, namesIn(rule(t, doc, sec, "R1")))
		compare(t, doc+" R2 (transient)", transients, namesIn(rule(t, doc, sec, "R2")))
	}
}

// declaredSets splits every declared Type by what IsTransient actually answers — the same question
// the store asks before it will persist one, rather than which const block the constant sits in.
// That distinction is not academic: `model.changed` sat under the "Transient events" header for
// most of its life while SetModel recorded it, and three documents copied the header.
func declaredSets(t *testing.T) (facts, transients []string) {
	t.Helper()
	src := readRepoFile(t, "internal/core/event/event.go")
	decl := regexp.MustCompile(`(?m)^\s*(Type\w+)\s+Type\s*=\s*"([^"]+)"`)
	for _, m := range decl.FindAllStringSubmatch(src, -1) {
		if Type(m[2]).IsTransient() {
			transients = append(transients, m[2])
		} else {
			facts = append(facts, m[2])
		}
	}
	if len(facts) == 0 || len(transients) == 0 {
		t.Fatalf("read no type constants out of event.go — the parse broke, not the docs")
	}
	return facts, transients
}

// namesIn pulls the backticked type names out of a rule. Capitalised identifiers (`Store`) and
// prose are left alone; a name is lowercase, dot-separated, and nothing else.
func namesIn(rule string) []string {
	var out []string
	for _, m := range regexp.MustCompile("`([a-z][a-z0-9]*(?:\\.[a-z0-9]+)*)`").FindAllStringSubmatch(rule, -1) {
		out = append(out, m[1])
	}
	return out
}

func compare(t *testing.T, where string, want, got []string) {
	t.Helper()
	have := map[string]bool{}
	for _, g := range got {
		have[g] = true
	}
	need := map[string]bool{}
	var missing []string
	for _, w := range want {
		need[w] = true
		if !have[w] {
			missing = append(missing, w)
		}
	}
	var surplus []string
	for _, g := range got {
		if !need[g] {
			surplus = append(surplus, g)
		}
	}
	sort.Strings(missing)
	sort.Strings(surplus)
	if len(missing) > 0 {
		t.Errorf("%s does not name %v — a client reading by this list meets those lines and cannot place them",
			where, missing)
	}
	if len(surplus) > 0 {
		t.Errorf("%s names %v, which the code does not declare — a client waits for lines that never come",
			where, surplus)
	}
}

// rule returns one "- R<n> …" bullet, including its continuation lines.
func rule(t *testing.T, doc, sec, id string) string {
	t.Helper()
	lines := strings.Split(sec, "\n")
	for i, l := range lines {
		if !strings.HasPrefix(strings.TrimSpace(l), "- "+id+" ") {
			continue
		}
		out := []string{l}
		for _, next := range lines[i+1:] {
			if strings.HasPrefix(strings.TrimSpace(next), "- ") || strings.TrimSpace(next) == "" {
				break
			}
			out = append(out, next)
		}
		return strings.Join(out, "\n")
	}
	t.Fatalf("%s: F-EVENT-FACT-TRANSIENT has no %s rule", doc, id)
	return ""
}

func section(t *testing.T, doc, body, head string) string {
	t.Helper()
	i := strings.Index(body, head)
	if i < 0 {
		t.Fatalf("%s: no %q section", doc, head)
	}
	rest := body[i+len(head):]
	if j := strings.Index(rest, "\n### "); j >= 0 {
		return rest[:j]
	}
	return rest
}

// readRepoFile reads a path relative to the repo root, found by walking up to the go.mod that
// owns this package. A test that hardcoded ../../../ would pass silently the day the package moves.
func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %s — cannot locate the repo root", dir)
		}
		dir = parent
	}
	b, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}
