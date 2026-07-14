package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvScanEnabledDefaultOff(t *testing.T) {
	t.Setenv("MAGI_ENV_SCAN", "")
	if envScanEnabled() {
		t.Fatal("MAGI_ENV_SCAN unset must be OFF (opt-in)")
	}
	t.Setenv("MAGI_ENV_SCAN", "1")
	if !envScanEnabled() {
		t.Fatal("MAGI_ENV_SCAN=1 must be ON")
	}
	t.Setenv("MAGI_ENV_SCAN", "off")
	if envScanEnabled() {
		t.Fatal("MAGI_ENV_SCAN=off must be OFF")
	}
}

func TestRepoContextTwoLevelAndAnchorExcerpt(t *testing.T) {
	dir := t.TempDir()
	// A build anchor at top level whose opening lines must surface.
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte("CC = gcc\nCFLAGS = -O2\nall:\n\t$(CC) $(CFLAGS) main.c\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A source dir with a nested file (two-level tree must show the child).
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "main.c"), []byte("int main(){return 0;}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Noise dir that must be listed but NOT descended into.
	if err := os.MkdirAll(filepath.Join(dir, "node_modules", "leftpad"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node_modules", "leftpad", "index.js"), []byte("module.exports=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Hidden entry must be skipped.
	if err := os.WriteFile(filepath.Join(dir, ".secret"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := repoContext(dir)

	if !strings.Contains(got, "Makefile") {
		t.Errorf("top-level Makefile missing:\n%s", got)
	}
	if !strings.Contains(got, "src/") || !strings.Contains(got, "main.c") {
		t.Errorf("two-level tree must show src/ and its child main.c:\n%s", got)
	}
	if !strings.Contains(got, "node_modules/") {
		t.Errorf("noise dir should still be listed:\n%s", got)
	}
	if strings.Contains(got, "leftpad") || strings.Contains(got, "index.js") {
		t.Errorf("noise dir must NOT be descended into:\n%s", got)
	}
	if strings.Contains(got, ".secret") {
		t.Errorf("hidden entry must be skipped:\n%s", got)
	}
	if !strings.Contains(got, "Makefile (excerpt)") || !strings.Contains(got, "CFLAGS = -O2") {
		t.Errorf("anchor excerpt must include Makefile opening lines:\n%s", got)
	}
}

func TestRepoContextUnavailable(t *testing.T) {
	if got := repoContext(filepath.Join(t.TempDir(), "does-not-exist")); got != "(unavailable)" {
		t.Errorf("missing workdir must yield (unavailable), got %q", got)
	}
}

func TestHeadExcerptLineCap(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "many.txt")
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, "line")
	}
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := headExcerpt(p, 5, 4096)
	if n := strings.Count(got, "\n"); n != 5 {
		t.Errorf("lineCap=5 must yield 5 lines, got %d:\n%q", n, got)
	}
	if headExcerpt(filepath.Join(dir, "nope"), 5, 4096) != "" {
		t.Error("unreadable file must yield empty excerpt")
	}
}
