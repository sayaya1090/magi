// Package spec holds docs/SPEC.md to the code it specifies.
//
// SPEC's preamble used to state a contract it did not keep: "The case id (`read-1` and so on) is
// used verbatim as the test name." That is how an outside reader — the console, a client in
// another language, whoever picks this up next — checks whether a rule is actually held: search
// the id.
//
// Measured 2026-08-29 against 88 case ids: 37 appeared in Go source as a name, 7 only in a comment,
// and 44 nowhere at all. The split falls exactly along the document's own history. Every F-TOOL and
// F-STORE case is linked, without exception; every section written after them is where the gaps
// are, F-COUNCIL at 0 of 26.
//
// The failure that causes is a false negative, and it is the worse direction. A reader who trusts
// the sentence searches for `compact-ctx-1`, finds nothing, and concludes the rule is unheld — when
// in most of these cases a test does hold it under a different name. They cannot tell "torn out"
// from "never held" from "held, not labelled", and the document tells them not to look further.
//
// So the sentence was narrowed to what can be true, and this test is what makes it true: Part A,
// which the document says is "in depth". Part B calls itself an outline and is excluded by that
// word. Keeping the promise in a test rather than in prose is the point — prose cannot notice the
// next case id that ships without one.
package spec

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// repoRoot walks up for go.mod rather than counting ../ from this file, so moving the package does
// not silently make the test read nothing and pass.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %s", dir)
		}
		dir = parent
	}
}

var caseID = regexp.MustCompile(`^([a-z][a-z0-9]*(?:-[a-z0-9]+)*-\d+):`)

type specCase struct {
	id      string
	section string
	partB   bool
}

// specCases reads every case id out of SPEC.md's fenced blocks, remembering which section it came
// from and which side of the Part A/Part B line it is on.
func specCases(t *testing.T, root string) []specCase {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "docs", "SPEC.md"))
	if err != nil {
		t.Fatalf("read SPEC.md: %v", err)
	}
	var out []specCase
	section, partB, inFence := "", false, false
	for _, line := range strings.Split(string(b), "\n") {
		switch {
		case strings.HasPrefix(line, "```"):
			inFence = !inFence
			continue
		case strings.HasPrefix(line, "# Part B"):
			partB = true
		case strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "### "):
			section = strings.Fields(strings.TrimLeft(line, "# "))[0]
		}
		if !inFence {
			continue
		}
		if m := caseID.FindStringSubmatch(line); m != nil {
			out = append(out, specCase{id: m[1], section: section, partB: partB})
		}
	}
	if len(out) == 0 {
		t.Fatal("no case ids found in SPEC.md — the fence or id shape changed and this test went blind")
	}
	return out
}

// goSources is every .go file in the repo, read once. Reading them all beats running a grep per id:
// there are dozens of ids and the whole tree is a few megabytes.
func goSources(t *testing.T, root string) string {
	t.Helper()
	var sb strings.Builder
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Vendored and build output carry no citations and cost seconds to walk.
			if n := d.Name(); n == ".git" || n == "vendor" || n == "node_modules" || n == "web" {
				return filepath.SkipDir
			}
			// This package is the instrument, not a citation. notLinkedYet spells every id it
			// knows about, so counting it as source would let each gap vouch for itself.
			if path == filepath.Join(root, "internal", "spec") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		c, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sb.Write(c)
		sb.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	return sb.String()
}

// notLinkedYet are Part A case ids that no Go source names, with the reason each one is still here.
//
// It is a list of known gaps, not a set of exemptions: a reader who searches for one of these and
// finds nothing can come here and learn WHY, which is the whole thing the preamble's claim takes
// away from them. Nothing may be added to it — a new case id ships with its citation — and the
// right way to remove one is to put the id in the test that already holds it.
var notLinkedYet = map[string]string{
	"fallback-1": "F-LLM-FALLBACK: the fenced tool_call shape. Parsed in the openai adapter; no test names the case.",
	"fallback-2": "F-LLM-FALLBACK: an ordinary sentence stays text.",
	"fallback-3": "F-LLM-FALLBACK: broken JSON gets one repair retry, then text.",
	"headless-1": "F-HEADLESS: -p with --output json. Exercised end to end by cmd/magi, not by an id-named case.",
	"headless-2": "F-HEADLESS: prompt piped in on stdin.",
	"headless-3": "F-HEADLESS: non-TTY output carries no ANSI.",
	"headless-4": "F-HEADLESS: an LLM error goes to stderr with a non-zero exit.",
	// Measured 2026-08-29 by reading every candidate, not by grepping for a likely name. These
	// three are gaps in the code, not in the labelling: no test holds them under any name.
	"headless-5": "F-HEADLESS: council feedback rendered line by line under the tally. The renderer " +
		"is held (headless-6); its one caller, cmd/magi/main.go's headless printer, is not.",
	"loop-int-1": "F-LOOP-INTERRUPT: interrupt mid-stream, text received so far persisted. Every " +
		"Interrupt test drives a session that is between turns, so the mid-stream path never runs.",
	"recon-1": "F-EVENT-RECON: a tool-call and its result reconstruct as an assistant message and " +
		"a tool message. Tests build that event shape (model_view_appendonly, elide) but assert " +
		"other properties of it; nothing asserts the message count or the two roles.",
}

// TestPartACasesAreCitedInCode holds SPEC's preamble to the repo: a Part A case id is searchable.
//
// Both directions are failures. An uncited id is a rule a reader cannot check. An entry in
// notLinkedYet that IS cited is a stale note claiming a gap that has since been closed, and a list
// of gaps that lies about one of them is no better than the sentence this test exists to replace.
func TestPartACasesAreCitedInCode(t *testing.T) {
	root := repoRoot(t)
	src := goSources(t, root)

	var missing, staleNote []string
	for _, c := range specCases(t, root) {
		if c.partB {
			continue
		}
		cited := strings.Contains(src, c.id)
		_, known := notLinkedYet[c.id]
		switch {
		case !cited && !known:
			missing = append(missing, c.section+" "+c.id)
		case cited && known:
			staleNote = append(staleNote, c.id)
		}
	}
	sort.Strings(missing)
	sort.Strings(staleNote)

	if len(missing) > 0 {
		t.Errorf("SPEC Part A case ids that no Go source names: %v\n"+
			"SPEC's preamble says the case id is used verbatim as the test name, so a reader checks a "+
			"rule by searching the id. These return nothing, and finding nothing reads as 'not held' "+
			"even when a test holds it under another name. Put the id in that test, or say why not in "+
			"notLinkedYet.", missing)
	}
	if len(staleNote) > 0 {
		t.Errorf("notLinkedYet still lists %v, but Go source names them now — delete those entries.", staleNote)
	}
}
