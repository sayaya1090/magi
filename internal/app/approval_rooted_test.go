package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The preview refuses what the tool's jail refuses — including the spelling the jail learned last.
//
// writeApprovalDiff exists to mirror resolvePath: an approval screen must not show bytes the real
// write was going to be refused for reading. The mirror was written from the jail as it stood, and
// then the jail learned that a ROOTED path is not a relative one — `/etc/passwd` and
// `\Windows\System32\…` carry no volume, so `filepath.IsAbs` calls them relative on Windows, and
// folding them into the workdir names a different file than the caller asked for.
//
// The copy kept folding. So the preview read `<workdir>\etc\passwd`, and put it on every status
// viewer labelled as `/etc/passwd` — the exact failure the function was written to prevent, one
// spelling over.
//
// No symlink is needed to show it, which matters: the symlink test beside this one skips wherever
// the OS will not grant the privilege, and that is most Windows machines.
func TestTheApprovalPreviewRefusesARootedPath(t *testing.T) {
	wd := t.TempDir()
	// A real file at the folded location, so a preview that folds has something to show. If the
	// refusal ever regresses, this is what lands on the screen.
	planted := filepath.Join(wd, "etc")
	if err := os.MkdirAll(planted, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planted, "passwd"), []byte("root:x:0:0\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for name, p := range map[string]string{
		"a unix absolute":       "/etc/passwd",
		"rooted with no volume": `\Windows\System32\drivers\etc\hosts`,
		"climbing out":          filepath.Join("..", "outside.txt"),
	} {
		args, err := json.Marshal(map[string]string{"path": p, "content": "x"})
		if err != nil {
			t.Fatal(err)
		}
		if diff, ok := writeApprovalDiff(wd, args); ok {
			t.Errorf("%s (%q): 미리보기가 무언가를 보여 줬다 — 부른 이름과 다른 파일이다:\n%s", name, p, diff)
		}
	}

	// And an honest path inside the workdir is still previewed, or the mirror is just a wall.
	if err := os.WriteFile(filepath.Join(wd, "notes.md"), []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	args, err := json.Marshal(map[string]string{"path": "notes.md", "content": "after\n"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := writeApprovalDiff(wd, args); !ok {
		t.Error("워크디렉토리 안의 평범한 쓰기는 여전히 미리보기가 나와야 한다")
	}
}

// The change record names the file the command named, not one folded into the workdir.
//
// Bash paths reach these without passing the file tools' jail, so a rooted target really does
// arrive. Folding it made the before/after capture read `<workdir>\etc\hosts` — and then say
// something about a file nobody had written.
func TestTheChangeRecordResolvesARootedPathWhereItPoints(t *testing.T) {
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	decoy := filepath.Join(wd, "etc", "hosts")
	if err := os.WriteFile(decoy, []byte("decoy\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// The folded location exists; the rooted one almost certainly does not. A resolver that folds
	// says "it is there" about a file the command never named.
	if pathExists(wd, "/etc/hosts") && !pathExists("", "/etc/hosts") {
		t.Error("뿌리 있는 경로를 워크디렉토리 안으로 접어 「있다」고 답했다 — 부르지 않은 파일이다")
	}
	// A plain relative path still resolves against the workdir.
	if !pathExists(wd, filepath.Join("etc", "hosts")) {
		t.Error("평범한 상대 경로는 워크디렉토리 기준으로 풀려야 한다")
	}
}
