package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The page's script has to be valid JavaScript, and nothing else here can tell.
//
// The front end is a Go string, so the compiler sees text and every Go test passes with a syntax
// error in it. What happens in the browser is that the whole script fails to parse and the page is
// blank — dashboard, transcript and composer at once, with a clean build behind it. The string has
// been edited by hand and by script a dozen times this week.
//
// Parsed with whatever runtime is on the machine and skipped when there is none: a check that can
// only run sometimes is worth more than no check, as long as it never guesses.
func TestThePageScriptParses(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node on this machine; nothing here can parse JavaScript")
	}
	body := scriptBody(t, indexHTML)
	if len(body) < 500 {
		t.Fatalf("the extracted script is %d bytes — the extraction is wrong, not the page", len(body))
	}
	f := filepath.Join(t.TempDir(), "page.mjs")
	if err := os.WriteFile(f, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// --check parses without executing: the page's script touches document and window, which do
	// not exist here, and running it would fail for reasons that are not about the code.
	out, err := exec.Command(node, "--check", f).CombinedOutput()
	if err != nil {
		t.Errorf("the page's script does not parse:\n%s", out)
	}
}

// Every element the script reaches for by id must exist in the markup.
//
// getElementById returning null is not a parse error — the page loads, and then the first line that
// touches the missing element throws, taking everything after it with it. Which is most of the page,
// because this script is one block.
func TestEveryElementTheScriptReachesForExists(t *testing.T) {
	body := scriptBody(t, indexHTML)
	for _, id := range idsUsed(body) {
		if !strings.Contains(indexHTML, `id="`+id+`"`) {
			t.Errorf("the script looks up #%s and the markup has no such element", id)
		}
	}
}

// scriptBody returns the contents of the page's <script> block.
func scriptBody(t *testing.T, page string) string {
	t.Helper()
	open := strings.Index(page, "<script>")
	closeAt := strings.LastIndex(page, "</script>")
	if open < 0 || closeAt < 0 || closeAt < open {
		t.Fatal("the page has no <script> block")
	}
	return page[open+len("<script>") : closeAt]
}

// idsUsed finds every getElementById('x') in the script.
func idsUsed(body string) []string {
	var out []string
	const call = "getElementById('"
	for i := 0; ; {
		j := strings.Index(body[i:], call)
		if j < 0 {
			return out
		}
		i += j + len(call)
		if k := strings.IndexByte(body[i:], '\''); k >= 0 {
			out = append(out, body[i:i+k])
		}
	}
}
