package app

import (
	"path/filepath"
	"strings"
	"testing"
)

// The path this seam CHECKS and the path its callers USE have to be the same file.
//
// insideWorkdir decided what to check with `filepath.IsAbs`, and on Windows that is not the same
// question as "is this path rooted". `/etc/hosts` and `\Windows\System32\…` are rooted and carry no
// volume, so IsAbs says false, the path gets joined to the workspace, and `C:\ws\etc\hosts` is
// comfortably inside it. The caller then hands the ORIGINAL string to git or to the OS, which
// resolves it against the current drive: `C:\etc\hosts`. One string, two files, and the gate looked
// at the wrong one.
//
// It is reachable: two of the five callers throw the resolved path away and use the caller's
// spelling, and `/diff` — one of them — is read-classified, which is what a console, a relay or a
// peer may ask for. Measured 2026-09-10 as "GitDiffOf read /etc/hosts from outside the workspace".
//
// The literals are deliberate rather than filepath.Join'd: these are the shapes an ATTACKER writes,
// and the point is that they must be refused however this platform spells its own paths.
func TestARootedPathIsNotTreatedAsRelativeToTheWorkspace(t *testing.T) {
	wd := t.TempDir()
	for name, path := range map[string]string{
		"a unix absolute":            "/etc/hosts",
		"a unix absolute, deeper":    "/var/run/secrets/token",
		"rooted with no volume":      `\Windows\System32\drivers\etc\hosts`,
		"rooted with a forward hop":  `\Windows/System32/config`,
		"climbing out":               filepath.Join("..", "elsewhere", "secret"),
		"climbing out through a dot": filepath.Join(".", "..", "secret"),
	} {
		got, err := insideWorkdir(wd, path)
		if err == nil {
			t.Errorf("%s (%q): 통과시켰다 — 해석한 것은 %q 인데 OS 는 다른 파일을 연다", name, path, got)
			continue
		}
		if !strings.Contains(err.Error(), "outside this workspace") {
			t.Errorf("%s (%q): 거절 이유가 %q — 밖이라고 말해야 한다", name, path, err)
		}
	}
}

// And an honest caller is still let in, or the gate is just a wall.
//
// A path INSIDE the workspace has to work spelled either way — relative, which is what a console
// sends, and absolute, which is what a tool that already resolved it sends.
func TestAPathInsideTheWorkspaceIsStillAllowed(t *testing.T) {
	wd := t.TempDir()
	for name, path := range map[string]string{
		"a plain file":        "main.go",
		"below a directory":   filepath.Join("src", "pkg", "a.go"),
		"with a leading dot":  filepath.Join(".", "main.go"),
		"down and back up":    filepath.Join("src", "..", "main.go"),
		"absolute and inside": filepath.Join(wd, "main.go"),
	} {
		got, err := insideWorkdir(wd, path)
		if err != nil {
			t.Errorf("%s (%q): 워크스페이스 안인데 거절했다: %v", name, path, err)
			continue
		}
		// What comes back is what the caller should USE — resolved, and under the workspace.
		rel, rerr := filepath.Rel(wd, got)
		if rerr != nil || escapesTree(rel) {
			t.Errorf("%s: 돌려준 경로가 워크스페이스 밖이다: %q", name, got)
		}
	}
	if _, err := insideWorkdir(wd, "   "); err == nil {
		t.Error("빈 경로는 어느 파일인지 안 말한 것이다 — 거절해야 한다")
	}
}
