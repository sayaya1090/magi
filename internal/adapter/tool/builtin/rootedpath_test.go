package builtin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A rooted path is not a relative path, and answering as if it were makes the tool lie.
//
// `filepath.IsAbs` says false for `/etc/passwd` and `\Windows\System32\…` on Windows, because
// neither carries a volume. resolvePath then joined them to the workdir, they landed inside it, the
// jail was satisfied — and the tool reported back the caller's spelling. Measured 2026-09-10:
// `write /etc/profile.d/sqlite.sh` answered "wrote 2 bytes to /etc/profile.d/sqlite.sh" having
// created `<workdir>\etc\profile.d\sqlite.sh`, and `list /etc` answered with that directory's
// contents as if they were the system's.
//
// Nothing escaped the jail. What broke is the sentence the model reads: it is told a machine-wide
// profile script now exists. The next turn is built on that.
//
// The literals are written out rather than composed with filepath.Join, because these are the
// shapes a CALLER writes and they must be refused however this platform spells its own paths.
func TestARootedPathIsRefusedRatherThanQuietlyMadeLocal(t *testing.T) {
	wd := t.TempDir()
	for name, p := range map[string]string{
		"a unix absolute":           "/etc/passwd",
		"a unix absolute, deeper":   "/etc/profile.d/sqlite.sh",
		"rooted with no volume":     `\Windows\System32\drivers\etc\hosts`,
		"rooted, separators mixed":  `\Windows/System32/config`,
		"climbing out":              "../outside.txt",
		"climbing out twice":        "../../outside.txt",
		"climbing out then back in": "../" + filepath.Base(wd) + "/ok.txt",
	} {
		got, err := resolvePath(wd, p)
		if err == nil {
			t.Errorf("%s (%q): 통과시켰다 — 부른 이름과 다른 파일 %q 를 만들게 된다", name, p, got)
			continue
		}
		if !strings.Contains(err.Error(), "outside workdir") {
			t.Errorf("%s (%q): 거절 이유가 %q — 감옥의 이름을 달고 있어야 한다", name, p, err)
		}
	}
}

// And the ordinary shapes still resolve, or the jail is a wall.
//
// `.` in particular: IsLocal is a narrow question and this is the one answer somebody would expect
// it to get wrong. It does not — but a rule this test does not state is a rule the next change can
// take away.
func TestTheOrdinaryShapesStillResolve(t *testing.T) {
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]string{
		"the workdir itself":    ".",
		"a file in it":          "a.txt",
		"with a leading dot":    "./a.txt",
		"below a directory":     "src/a.txt",
		"with the OS separator": filepath.Join("src", "a.txt"),
		"a dot in the middle":   "src/./a.txt",
		"down and back up":      "src/../a.txt",
		"a name with a space":   "my file.txt",
		"absolute and inside":   filepath.Join(wd, "a.txt"),
	} {
		got, err := resolvePath(wd, p)
		if err != nil {
			t.Errorf("%s (%q): 워크디렉토리 안인데 거절했다: %v", name, p, err)
			continue
		}
		rel, rerr := filepath.Rel(wd, got)
		if rerr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			t.Errorf("%s: 돌려준 경로가 밖이다: %q", name, got)
		}
	}
}
