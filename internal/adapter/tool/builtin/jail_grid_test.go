package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	for _, c := range []struct {
		name, tool, args string
	}{
		{"read a symlinked file", "read", `{"path":"link.txt"}`},
		{"read through a symlinked directory", "read", `{"path":"linkdir/secret.txt"}`},
		{"read up and out", "read", `{"path":"../outside/secret.txt"}`},
		{"read an absolute path outside", "read", `{"path":"` + secret + `"}`},
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
	} {
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
