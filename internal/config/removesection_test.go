package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/config"
)

// Removing an MCP server means removing its whole table: a caller cannot clear it key by key,
// because a table somebody else wrote may hold keys this build does not know about.
func TestRemoveSectionTakesTheWholeTableAndItsExplanation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	const before = `model = "qwen3"

# The design team's Figma bridge.
[mcp.figma]
command = "npx"
args = ["-y", "figma-mcp"]

[mcp.docs]
url = "http://localhost:3000/mcp"

[routing]
reviewer = "gpt-oss:120b"
`
	if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.RemoveSection(path, "mcp.figma"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	for _, gone := range []string{"figma", "npx", "Figma bridge"} {
		if strings.Contains(s, gone) {
			t.Errorf("%q survived:\n%s", gone, s)
		}
	}
	// Everything else is untouched, comments and spacing included.
	for _, kept := range []string{`model = "qwen3"`, "[mcp.docs]", "http://localhost:3000/mcp", "[routing]"} {
		if !strings.Contains(s, kept) {
			t.Errorf("%q was taken with it:\n%s", kept, s)
		}
	}
	if strings.Contains(s, "\n\n\n") {
		t.Errorf("the removal left a hole:\n%s", s)
	}
	// And the file still parses as what it says it is.
	c, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, still := c.MCP["figma"]; still {
		t.Error("the parsed config still has the server")
	}
	if _, ok := c.MCP["docs"]; !ok {
		t.Error("the other server was lost")
	}

	// Asking twice, or for one that was never there, is not an error: the caller asked for it to
	// be gone and it is gone.
	if err := config.RemoveSection(path, "mcp.figma"); err != nil {
		t.Errorf("removing it again failed: %v", err)
	}
	if err := config.RemoveSection(filepath.Join(dir, "nothing.toml"), "mcp.x"); err != nil {
		t.Errorf("removing from a missing file failed: %v", err)
	}
}
