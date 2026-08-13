package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
		// The icon symbols are the one set of ids that is allowed to be absent. They come from a
		// sprite baked in at build time from a licensed download, so a build without that licence
		// has none of them — and the page is written to ask (see icon() in page.js) rather than to
		// assume, which is exactly the case this check would otherwise forbid.
		if strings.HasPrefix(id, "i-") {
			continue
		}
		if !strings.Contains(indexHTML, `id="`+id+`"`) {
			t.Errorf("the script looks up #%s and the markup has no such element", id)
		}
	}
}

// scriptBody returns the contents of the page's script block, module or not.
//
// It became `type="module"` the day the page started importing the vendored bundle, and a matcher
// pinned to the bare tag reported "the page has no script" for a page whose script had simply
// gained an attribute.
func scriptBody(t *testing.T, page string) string {
	t.Helper()
	// The MODULE, not the first script on the page. There is a four-line one in the head that
	// applies a stored theme before the stylesheet paints, and taking from the first <script to the
	// last </script> swept it and the markup between them into what node was asked to run.
	openAt := strings.Index(page, `<script type="module">`)
	if openAt < 0 {
		t.Fatal("the page has no module script")
	}
	openAt += len(`<script type="module">`)
	closeAt := strings.LastIndex(page, "</script>")
	if closeAt < openAt {
		t.Fatal("the module script is unterminated")
	}
	return page[openAt:closeAt]
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

// An HTMLCollection is not an array, and the fake DOM's is.
//
// `children` comes back as an HTMLCollection in a browser: it has length and indexing and NONE of
// the array methods. The fake here answers with a real array, so a page written against it passes
// every test and throws on the first frame in front of a person — and because most of this page is
// painted from one function, an exception in the middle of it leaves everything after that line
// blank. That is exactly how the navigation lost its labels: not a styling problem, a TypeError
// three lines above the loop that writes them. Second time; the first was `.indexOf`.
//
// A source scan rather than a richer fake: making children array-like would break the tests that
// read it, and this is the cheaper guard for a mistake whose whole shape is one property access.
func TestThePageDoesNotTreatAChildListAsAnArray(t *testing.T) {
	// Comments stripped first: this file explains the mistake in prose more than once, and a scan
	// that reads its own warning as an instance of the thing it warns about cries wolf forever.
	body := regexp.MustCompile(`(?m)^\s*//.*$`).ReplaceAllString(scriptBody(t, indexHTML), "")
	call := regexp.MustCompile(`\.children\s*(\|\|\s*\[\])?\s*\)?\.(some|map|filter|forEach|reduce|find|includes|indexOf|slice|sort|every|flatMap|at)\b`)
	// A copy is fine, and both ways of making one are: Array.from(...) and a spread. Looking back a
	// few characters rather than writing one regex for all three — Go has no lookbehind, and a
	// pattern that tried would be the kind nobody can read or fix.
	safe := func(before string) bool {
		return strings.Contains(before, "Array.from(") || strings.Contains(before, "...(")
	}
	for _, at := range call.FindAllStringIndex(body, -1) {
		from := at[0] - 24
		if from < 0 {
			from = 0
		}
		if safe(body[from:at[0]]) {
			continue
		}
		t.Errorf("an array method is called on a child list: %q — wrap it in Array.from()",
			body[at[0]:at[1]])
	}
	// The scan has to be able to fail: a pattern that matches nothing because it is wrong reads
	// exactly like a page that is clean.
	if !call.MatchString("const n = box.children.map(x => x);") {
		t.Error("the scan does not recognise the mistake it exists to find")
	}
}

// A companion is named once in a request, not twice.
//
// post() addresses a companion from the socket and peer it is handed, and four call sites ALSO
// appended ?d= to the path: /save, /file-do and both of the git ones. The result was
// /save?d=…sock?d=…sock, which the server reads as a socket path with a query welded to the end —
// so it answered "no daemon at …?d=…" and every workspace-changing action from this page silently
// did nothing. Nothing could see it: the demo mocks every POST and the Go tests call handlers
// directly, so the URL the page builds was never exercised until a live console was in front of a
// real daemon.
func TestARequestNamesItsCompanionOnce(t *testing.T) {
	src := scriptBody(t, indexHTML)
	// post(path, ...) — the path may not carry a query, because post appends its own.
	doubled := regexp.MustCompile(`post\(\s*'[^']*'\s*\+\s*q(For)?\(`)
	if m := doubled.FindAllString(src, -1); len(m) > 0 {
		t.Errorf("%d call site(s) hand post() a path that already names the companion: %v", len(m), m)
	}
	// And the same shape through the other sender, which appends q() when the path has no query.
	if m := regexp.MustCompile(`postText\(\s*'[^']*\?[^']*'\s*\+\s*q\(`).FindAllString(src, -1); len(m) > 0 {
		t.Errorf("postText is given a path with two queries: %v", m)
	}
}
