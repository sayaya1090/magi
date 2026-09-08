package idebridge

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The contract document lists the methods that are built. That list ages the moment one is added
// here, and a document that names a door this build does not answer sends somebody to write a
// client against it — the same failure the advertisement in `about` exists to prevent, one layer
// out. So the document is checked against the code rather than trusted.
//
// Both halves matter and they fail differently: a method in the code but not the document is a
// door nobody knows about, and a method in the document but not the code is a door somebody calls
// and never gets an answer from.
func TestBothContractDocsListExactlyWhatIsBuilt(t *testing.T) {
	for _, name := range []string{"IDE_BRIDGE.md", "IDE_BRIDGE.ko.md"} {
		path := filepath.Join("..", "..", "..", "docs", name)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		built := builtSection(t, name, string(body))
		listed := map[string]bool{}
		// Rows of the built table: | `method` | what it does |
		for _, m := range regexp.MustCompile("(?m)^\\|\\s*`([a-z-]+)`\\s*\\|").FindAllStringSubmatch(built, -1) {
			listed[m[1]] = true
		}
		if len(listed) == 0 {
			t.Fatalf("%s: found no method rows in the built table — the guard is not reading it", name)
		}
		for _, m := range Methods() {
			if !listed[m] {
				t.Errorf("%s does not list %q, which this build answers", name, m)
			}
			delete(listed, m)
		}
		for m := range listed {
			t.Errorf("%s lists %q as built, but this build does not answer it", name, m)
		}
	}
}

// builtSection is the text between the "built" table's heading and the "not built yet" one. Taking
// the whole file would let a method named in the NOT-built table count as listed, and the guard
// would pass while the document said the opposite of the truth.
func builtSection(t *testing.T, name, body string) string {
	t.Helper()
	start := strings.Index(body, "### ")
	if start < 0 {
		t.Fatalf("%s: no method section", name)
	}
	rest := body[start:]
	for _, end := range []string{"**Not built yet.**", "**아직 안 지어진 것.**"} {
		if i := strings.Index(rest, end); i >= 0 {
			return rest[:i]
		}
	}
	t.Fatalf("%s: no 'not built yet' heading — the two tables cannot be told apart", name)
	return ""
}
