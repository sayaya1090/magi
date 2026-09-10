package app

import (
	"path/filepath"
	"testing"
)

// A rooted target is judged where it points, not folded into the workspace.
//
// `filepath.IsAbs` asks whether a path is COMPLETE. On Windows a path can be rooted and incomplete
// at once — `/server`, `\Windows\System32` — so IsAbs answers false and the old reading joined it
// to the workdir. Somebody else's tree became a subdirectory of ours, and the gate that asks the
// council before an irreversible delete outside the workspace simply did not open.
//
// Measured 2026-09-10: `rm -rf /server` was gated on Unix and let through on Windows, where the
// shell resolves it against the current drive and deletes `C:\server`. It is also the shape a model
// writes without thinking about the platform, because it is how the command is written everywhere
// else.
//
// The targets are literals. They are what a CALLER writes, and they have to be read the same way
// however this platform spells its own paths.
func TestARootedTargetIsNotFoldedIntoTheWorkspace(t *testing.T) {
	const wd = "/app"
	for name, target := range map[string]string{
		"somebody else's tree":     "/server",
		"a file in it":             "/server/index.html",
		"a rooted windows path":    `\Windows\System32`,
		"rooted, mixed separators": `\Windows/System32`,
		"climbing out":             "../elsewhere",
		"the home directory":       "~",
	} {
		if !outsideWorkspace(wd, target) {
			t.Errorf("%s (%q): 워크스페이스 안이라고 읽었다 — 되돌릴 수 없는 삭제의 문이 안 열린다", name, target)
		}
	}

	// And what IS in the tree stays in it, or the gate asks about every ordinary cleanup.
	for name, target := range map[string]string{
		"a directory in it": "build",
		"a file in it":      "dist/app.js",
		"the tree itself":   ".",
		"down and back up":  "build/../dist",
	} {
		if outsideWorkspace(wd, target) {
			t.Errorf("%s (%q): 트리 안인데 밖이라고 읽었다", name, target)
		}
	}
}

// The scratch exemption has to know what THIS platform calls its temp area.
//
// The list was `/tmp`, `/var/tmp` and `TMPDIR` — the Unix spellings. Windows names it TEMP/TMP, and
// `os.TempDir` is what reads them, so on that platform nothing ever matched: a path in the run's
// OWN scratch space was judged to be somebody else's and gated. That is the safe direction, and it
// still costs a council call and a turn each time, spent on the one place this file says nobody
// else owns.
func TestTheScratchExemptionKnowsThisPlatformsTempArea(t *testing.T) {
	const wd = "/app"
	tmp := filepath.Join(t.TempDir(), "thing")
	if !isScratchPath(wd, tmp) {
		t.Errorf("이 플랫폼의 임시 영역 안인데 남의 것으로 읽었다: %q", tmp)
	}
	// The area itself is not one thing inside it.
	for _, root := range scratchRoots() {
		if isScratchPath(wd, root) {
			t.Errorf("임시 영역 자체는 여전히 문을 거쳐야 한다: %q", root)
		}
	}
}
