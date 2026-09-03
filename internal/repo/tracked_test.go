// Package repo holds guards about the repository itself rather than about what it builds.
package repo

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A compiled binary has been swept into a commit three times now: helper.exe (32MB, 2026-09-02),
// a macOS console binary before that, and clients/web/server/server (19MB) on 2026-09-03. Each time
// the fix was to add that one name to .gitignore, and each time the next `go build` dropped its
// output somewhere the list did not name — `go build ./clients/web/server` run from inside that
// directory writes a file called `server`, which no extension rule can catch.
//
// So this stops naming them. Every tracked file is read far enough to see whether it starts with an
// executable's magic number, which is the property all three shared and no source file has. A
// binary that reaches history stays in history even after it is deleted.
func TestNoCompiledBinaryIsTracked(t *testing.T) {
	root, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skipf("not a git checkout: %v", err)
	}
	top := strings.TrimSpace(string(root))

	out, err := exec.Command("git", "-C", top, "ls-files", "-z").Output()
	if err != nil {
		t.Fatalf("listing tracked files: %v", err)
	}

	// Mach-O (both byte orders, 32- and 64-bit, plus the fat/universal wrapper), ELF, and PE.
	magic := [][]byte{
		{0xfe, 0xed, 0xfa, 0xce}, {0xce, 0xfa, 0xed, 0xfe},
		{0xfe, 0xed, 0xfa, 0xcf}, {0xcf, 0xfa, 0xed, 0xfe},
		{0xca, 0xfe, 0xba, 0xbe}, {0xbe, 0xba, 0xfe, 0xca},
		{0x7f, 'E', 'L', 'F'},
		{'M', 'Z'},
	}
	var found []string
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel == "" {
			continue
		}
		f, err := os.Open(filepath.Join(top, rel))
		if err != nil {
			continue // a file listed but not checked out here is not this guard's business
		}
		var head [4]byte
		n, _ := f.Read(head[:])
		f.Close()
		for _, m := range magic {
			if n >= len(m) && bytes.HasPrefix(head[:n], m) {
				found = append(found, rel)
				break
			}
		}
	}
	if len(found) > 0 {
		t.Fatalf("compiled binaries are tracked: %v\n"+
			"A build dropped one where `git add -A` could reach it. Remove it from the index and "+
			"add its path to .gitignore — and note that deleting it later does not take it out of "+
			"history, which is why this fails now rather than at review.", found)
	}
}
