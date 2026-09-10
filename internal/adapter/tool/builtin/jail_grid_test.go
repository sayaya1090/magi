package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/testenv"

	"github.com/sayaya1090/magi/internal/port"
)

// The file tools are pure Go: no OS sandbox stands behind them, so their own path jail IS the
// boundary. Three escapes were found and fixed one at a time (a write through a broken symlink, a
// grep walk that read a symlinked file, a snippet tool that did the same), each by chasing the
// class into the next surface. This is that chase made permanent: every tool that resolves a path,
// against every shape that has ever been used to leave a workdir.
//
// A leak here is a real one — the sentinel is the content of a file outside the workspace.
func TestPathJailHoldsAcrossEveryFileTool(t *testing.T) {
	testenv.NeedSymlink(t)
	const sentinel = "TOPSECRET_TOKEN=abc123"
	root := t.TempDir()
	work := filepath.Join(root, "work")
	outside := filepath.Join(root, "outside")
	mk := func(dir string) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mk(work)
	mk(outside)
	mk(filepath.Join(work, "sub"))
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte(sentinel+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "inside.txt"), []byte("ordinary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, l := range [][2]string{
		{secret, filepath.Join(work, "link.txt")},        // symlinked FILE
		{outside, filepath.Join(work, "linkdir")},        // symlinked DIRECTORY
		{secret, filepath.Join(work, "sub", "deep.txt")}, // one level down, for the walkers
	} {
		if err := os.Symlink(l[0], l[1]); err != nil {
			t.Fatal(err)
		}
	}
	env := port.ToolEnv{Workdir: work}

	body := func(raw json.RawMessage) string {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s
		}
		return string(raw)
	}
	for _, c := range jailCases(secret) {
		// The fixture is checked where TestEveryJailCaseIsAskable explains it: a case that dies on
		// its own arguments satisfies every assertion below without reaching the tool.
		if !json.Valid([]byte(c.args)) {
			t.Errorf("%s: 인자가 JSON 이 아니다", c.name)
			continue
		}
		var got string
		raw := json.RawMessage(c.args)
		switch c.tool {
		case "read":
			r, _ := Read{}.Execute(context.Background(), raw, env)
			got = body(r.Content)
		case "grep":
			r, _ := Grep{}.Execute(context.Background(), raw, env)
			got = body(r.Content)
		case "glob":
			r, _ := Glob{}.Execute(context.Background(), raw, env)
			got = body(r.Content)
		case "list":
			r, _ := List{}.Execute(context.Background(), raw, env)
			got = body(r.Content)
		case "write":
			r, _ := Write{}.Execute(context.Background(), raw, env)
			got = body(r.Content)
		case "edit":
			r, _ := Edit{}.Execute(context.Background(), raw, env)
			got = body(r.Content)
		case "multiedit":
			r, _ := MultiEdit{}.Execute(context.Background(), raw, env)
			got = body(r.Content)
		}
		if strings.Contains(got, sentinel) {
			t.Errorf("%s LEAKED content from outside the workdir:\n%s", c.name, got)
		}
	}

	// Nothing outside was written either: the file still holds what it held, and no new one appeared.
	after, err := os.ReadFile(secret)
	if err != nil || !strings.Contains(string(after), sentinel) {
		t.Errorf("the file outside the workdir was modified: %q (%v)", string(after), err)
	}
	if _, err := os.Stat(filepath.Join(outside, "pwned.txt")); err == nil {
		t.Error("a write escaped the workdir and created a file outside it")
	}
	// And the jail did not cost the ordinary case: a real file inside is still readable.
	r, _ := Read{}.Execute(context.Background(), json.RawMessage(`{"path":"inside.txt"}`), env)
	if !strings.Contains(body(r.Content), "ordinary") {
		t.Errorf("a file inside the workdir must still be readable: %s", body(r.Content))
	}
}

// jsonPath renders a filesystem path as a JSON string — quotes included, so it drops into an args
// literal where the value goes.
//
// A path is the one value in these fixtures that cannot be pasted between quotes: on Windows it
// carries backslashes and json reads those as escapes. Marshalling is the only spelling that is
// right on both platforms, and it is a function rather than a comment because the next fixture
// needing a path will reach for the nearest example.
func jsonPath(p string) string {
	b, err := json.Marshal(p)
	if err != nil { // a string always marshals; if it ever does not, the fixture is the bug
		panic(err)
	}
	return string(b)
}

// jailCase is one call made at the jail's wall.
type jailCase struct{ name, tool, args string }

// jailCases is every shape that has ever been used to leave a workdir, as arguments.
//
// Lifted out of the test so it can be checked by something that does not need symlinks — see
// TestEveryJailCaseIsAskable — and written once so the two cannot drift.
func jailCases(secret string) []jailCase {
	return []jailCase{
		{"read a symlinked file", "read", `{"path":"link.txt"}`},
		{"read through a symlinked directory", "read", `{"path":"linkdir/secret.txt"}`},
		{"read up and out", "read", `{"path":"../outside/secret.txt"}`},
		{"read an absolute path outside", "read", `{"path":` + jsonPath(secret) + `}`},
		{"grep the whole tree", "grep", `{"pattern":"TOPSECRET_TOKEN","path":"."}`},
		{"grep a symlinked file", "grep", `{"pattern":"TOPSECRET_TOKEN","path":"link.txt"}`},
		{"grep a symlinked directory", "grep", `{"pattern":"TOPSECRET_TOKEN","path":"linkdir"}`},
		{"grep a subdir holding a symlink", "grep", `{"pattern":"TOPSECRET_TOKEN","path":"sub"}`},
		{"glob everything", "glob", `{"pattern":"**/*.txt"}`},
		{"list the root", "list", `{"path":"."}`},
		{"list a symlinked directory", "list", `{"path":"linkdir"}`},
		{"write over a symlink", "write", `{"path":"link.txt","content":"PWNED\n"}`},
		{"write up and out", "write", `{"path":"../outside/pwned.txt","content":"PWNED\n"}`},
		{"edit through a symlink", "edit", `{"path":"link.txt","old":"TOPSECRET","new":"PWNED"}`},
		{"multiedit through a symlink", "multiedit", `{"path":"link.txt","edits":[{"old":"TOPSECRET","new":"PWNED"}]}`},
	}
}

// Every case in the grid must be askable — that is, be arguments a tool will actually look at.
//
// ⚠ **A case that dies on its own arguments proves nothing, and proves it silently.** Every
// assertion in the grid is "the answer does not contain the sentinel", so a call that never reaches
// the tool satisfies it. One did: `{"path":"` + secret + `"}` pastes an absolute path into a JSON
// string literal, which is harmless while the path is POSIX and invalid the moment it is
// `C:\Users\…` — json refuses `\U` as an escape sequence, and the read comes back `invalid
// arguments` before the jail has been consulted. Measured on Windows 2026-09-11: the strongest case
// in that grid, an absolute path leading straight out of the workdir, was a no-op.
//
// It is a test of its own, and NOT a check inside the grid's loop, because the grid needs symlinks
// and skips where it cannot make them — which on Windows is the ordinary case, no privilege held.
// A fixture check that only runs where the fixture already works is not a check. This one runs
// everywhere, and it runs on the platform whose paths are the reason it exists.
func TestEveryJailCaseIsAskable(t *testing.T) {
	// An absolute path shaped like this machine's, which is the whole point of asking here.
	secret := filepath.Join(t.TempDir(), "outside", "secret.txt")
	seen := map[string]bool{}
	for _, c := range jailCases(secret) {
		if !json.Valid([]byte(c.args)) {
			t.Errorf("%s: 인자가 JSON 이 아니다 — 이 칸은 담장이 아니라 제 인자에 걸려 통과한다: %s",
				c.name, c.args)
		}
		if seen[c.name] {
			t.Errorf("%q 가 두 번 있다 — 하나는 다른 칸을 덮어쓴 이름이다", c.name)
		}
		seen[c.name] = true
	}
	// And the case this exists for is still in the table, named outright: a check that only reads
	// its own list would pass just as well with the entry deleted.
	var absolute bool
	for _, c := range jailCases(secret) {
		var arg struct {
			Path string `json:"path"`
		}
		// Read back through json, which is how the tool reads it: the literal holds the path
		// ESCAPED, so looking for the raw string in the fixture text finds nothing on Windows —
		// the same mistake this test was written about, one layer up.
		if json.Unmarshal([]byte(c.args), &arg) == nil && arg.Path == secret {
			absolute = true
		}
	}
	if !absolute {
		t.Error("워크디렉터리 밖 절대 경로를 그대로 읽어 보는 칸이 없다 — 담장이 가장 곧게 시험받는 자리다")
	}
}
