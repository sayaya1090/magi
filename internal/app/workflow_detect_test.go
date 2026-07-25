package app

import (
	"os"
	"path/filepath"
	"testing"
)

// detectVerifyCmd guesses a build+test command from project marker files in a FIXED priority order
// (go → cargo → python → npm → make). This locks each marker's mapping, the priority — a polyglot repo
// carrying both go.mod and package.json resolves to Go because go.mod is checked first, and package.json
// beats a bare Makefile — and the empty result for an unrecognized tree (which makes runWorkflow fall
// back to a model-run verify with no deterministic gate).
func TestDetectVerifyCmd(t *testing.T) {
	mk := func(files ...string) string {
		d := t.TempDir()
		for _, f := range files {
			if err := os.WriteFile(filepath.Join(d, f), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return d
	}
	cases := []struct {
		name  string
		files []string
		want  string
	}{
		{"go", []string{"go.mod"}, "go build ./... && go test ./..."},
		{"cargo", []string{"Cargo.toml"}, "cargo test"},
		{"python-pyproject", []string{"pyproject.toml"}, "python -m pytest -q"},
		{"python-pytest-ini", []string{"pytest.ini"}, "python -m pytest -q"},
		{"python-setup-py", []string{"setup.py"}, "python -m pytest -q"},
		{"npm", []string{"package.json"}, "npm test --silent"},
		{"make", []string{"Makefile"}, "make test"},
		{"polyglot: go.mod wins over package.json", []string{"go.mod", "package.json"}, "go build ./... && go test ./..."},
		{"package.json wins over a bare Makefile", []string{"package.json", "Makefile"}, "npm test --silent"},
		{"unrecognized tree → empty (model-run verify, no hard gate)", []string{"README.md"}, ""},
	}
	for _, c := range cases {
		if got := detectVerifyCmd(mk(c.files...)); got != c.want {
			t.Errorf("%s: detectVerifyCmd = %q, want %q", c.name, got, c.want)
		}
	}
}
