package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A workspace is trusted because somebody said so, and stays trusted across runs.
func TestTrustRemembersAWorkspace(t *testing.T) {
	cfg, wd := t.TempDir(), t.TempDir()
	if Trusted(cfg, wd) {
		t.Fatal("a workspace nobody has allowed came back trusted")
	}
	if already, err := Trust(cfg, wd); err != nil || already {
		t.Fatalf("Trust: already=%v err=%v", already, err)
	}
	if !Trusted(cfg, wd) {
		t.Error("a workspace that was just allowed is not trusted")
	}
	// Saying it twice is not an error and does not write a second line.
	if already, err := Trust(cfg, wd); err != nil || !already {
		t.Errorf("second Trust: already=%v err=%v", already, err)
	}
	if got := TrustedList(cfg); len(got) != 1 {
		t.Errorf("the list holds %v", got)
	}
	if was, err := Untrust(cfg, wd); err != nil || !was {
		t.Fatalf("Untrust: was=%v err=%v", was, err)
	}
	if Trusted(cfg, wd) {
		t.Error("a workspace that was taken off the list is still trusted")
	}
}

// One directory, one entry — whichever name it was reached by.
//
// The daemon's own socket path resolves symlinks for exactly this reason: on macOS a shell in
// /tmp/x and a process that chdir'd report /tmp/x and /private/tmp/x for one directory. A trust
// granted through one spelling and checked through the other would silently mean nothing, which is
// the worst way for a permission to fail.
func TestTrustResolvesTheSameDirectorySpeltTwoWays(t *testing.T) {
	cfg := t.TempDir()
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if _, err := Trust(cfg, link); err != nil {
		t.Fatal(err)
	}
	if !Trusted(cfg, real) {
		t.Error("trusted through the link, not trusted through the directory it points at")
	}
}

// The file is the operator's: comments and their own spacing survive a removal.
func TestUntrustKeepsWhatAPersonWrote(t *testing.T) {
	cfg, wd := t.TempDir(), t.TempDir()
	body := "# the ones I actually work in\n" + wd + "\n\n# (nothing else yet)\n"
	if err := os.WriteFile(filepath.Join(cfg, TrustFile), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if was, err := Untrust(cfg, wd); err != nil || !was {
		t.Fatalf("Untrust: was=%v err=%v", was, err)
	}
	got, err := os.ReadFile(filepath.Join(cfg, TrustFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# the ones I actually work in", "# (nothing else yet)"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("removing an entry rewrote the file:\n%s", got)
		}
	}
}
