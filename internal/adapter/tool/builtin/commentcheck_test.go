package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

func TestCommentNoiseAdvisory(t *testing.T) {
	// Narration and placeholder comments are flagged.
	noise := []string{
		"// I've updated the loop to be faster",
		"# rest of the code unchanged",
		"// ... your implementation here",
		"<!-- Note: changed the layout -->",
		"-- this function now returns early",
		"// ...",
	}
	for _, c := range noise {
		if commentNoiseAdvisory(c, "") == "" {
			t.Errorf("expected %q to be flagged as noise", c)
		}
	}
	// Legitimate intent comments and code are NOT flagged.
	fine := []string{
		"// guard against a nil session before the first turn",
		"# retry with backoff: the gateway 502s under cold start",
		"x := computeTotal()  // includes tax",
		"return errors.New(\"boom\")",
		"// TODO: revisit once the API stabilizes",
		"func main() {",
		"    counter += 1",
	}
	for _, c := range fine {
		if adv := commentNoiseAdvisory(c, ""); adv != "" {
			t.Errorf("false positive on %q: %s", c, adv)
		}
	}
}

// A comment already present in the prior text is not re-flagged — only newly
// added noise counts.
func TestCommentNoiseSkipsPreexisting(t *testing.T) {
	prior := "// I've done this already\nx := 1\n"
	added := "// I've done this already\nx := 2\n"
	if adv := commentNoiseAdvisory(added, prior); adv != "" {
		t.Errorf("pre-existing comment must not be flagged: %s", adv)
	}
}

// The advisory rides along on a successful edit result without blocking it.
func TestEditAppendsCommentAdvisory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.go")
	os.WriteFile(path, []byte("package p\n\nfunc f() {}\n"), 0o644)
	raw, _ := json.Marshal(editArgs{
		Path: "c.go",
		Old:  "func f() {}",
		New:  "func f() {\n\t// I've added a body here\n\treturn\n}",
	})
	res, _ := Edit{}.Execute(context.Background(), raw, port.ToolEnv{Workdir: dir})
	if res.IsError {
		t.Fatalf("edit failed: %s", res.Content)
	}
	if !strings.Contains(string(res.Content), "note: added comment") {
		t.Errorf("expected a comment advisory in %q", res.Content)
	}
	// The edit still applied.
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "return") {
		t.Errorf("edit must still apply despite the advisory; file=%q", b)
	}
}
