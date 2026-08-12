package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// No two flags share a name.
//
// Go's flag package panics on a redefinition, at REGISTRATION — so a duplicate does not break the
// command that added it, it breaks every command in the binary, on every run, before anything is
// parsed. It also survives a build and a test suite that never calls the flag path: this was added
// after `--join` was declared twice and the binary panicked on `--help`.
//
// Read from the source rather than by registering them, because registering them twice in one
// process is the panic being tested for.
func TestNoFlagIsDeclaredTwice(t *testing.T) {
	decl := regexp.MustCompile(`flag\.(?:String|Bool|Int|Duration|Float64)\(\s*"([^"]+)"`)
	seen := map[string]string{}
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".go") || strings.HasSuffix(f.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(f.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range decl.FindAllStringSubmatch(string(src), -1) {
			if where, dup := seen[m[1]]; dup {
				t.Errorf("--%s is declared in %s and again in %s; the flag package panics on that, "+
					"in every command this binary has", m[1], where, f.Name())
				continue
			}
			seen[m[1]] = f.Name()
		}
	}
	if len(seen) < 20 {
		t.Errorf("only %d flags were found, so this is not reading the source it thinks it is", len(seen))
	}
}
