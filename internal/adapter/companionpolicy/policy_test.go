package companionpolicy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePolicy(t *testing.T, cfg, wd, root, extra string) string {
	t.Helper()
	path, err := Path(cfg, wd)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("version=1\npurpose='Inspect plugin sources'\ninstructions=['Do not deploy']\n[[operations]]\nid='source'\ndescription='Read published sources'\nkind='read_file'\nroot=%q\n%s", filepath.ToSlash(root), extra)
	if err = os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestPolicyContractAndRevocation(t *testing.T) {
	cfg, wd, root := t.TempDir(), t.TempDir(), t.TempDir()
	c, err := Load(cfg, wd)
	if err != nil || len(c.Operations) != 0 {
		t.Fatalf("absent policy: %+v %v", c, err)
	}
	// A project file alone is not authority.
	os.MkdirAll(filepath.Join(wd, ".magi"), 0700)
	os.WriteFile(filepath.Join(wd, ".magi", "companion.toml"), []byte("version=1"), 0600)
	c, err = Load(cfg, wd)
	if err != nil || len(c.Operations) != 0 {
		t.Fatal(c, err)
	}
	p := writePolicy(t, cfg, wd, root, "")
	c, err = Load(cfg, wd)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(Guide(c), "Do not deploy") {
		t.Fatal("own guide lost instructions")
	}
	if len(Summaries(c).Operations[0].InputSchema) != 0 || len(c.Operations[0].InputSchema) == 0 {
		t.Fatal("summary mutated complete contract")
	}
	if _, err = Find(c, "source"); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if _, err = Call(context.Background(), cfg, wd, "source", json.RawMessage(`{"path":"a"}`)); err == nil {
		t.Fatal("removed policy still grants access")
	}
}
func TestPolicyRejectsInvalidDeclarations(t *testing.T) {
	cfg, wd, root := t.TempDir(), t.TempDir(), t.TempDir()
	p := writePolicy(t, cfg, wd, root, "")
	valid, _ := os.ReadFile(p)
	cases := map[string]string{"version": strings.Replace(string(valid), "version=1", "version=2", 1), "unknown": string(valid) + "typo=true\n", "kind": strings.Replace(string(valid), "read_file", "bash", 1), "root": strings.Replace(string(valid), fmt.Sprintf("%q", filepath.ToSlash(root)), "'relative'", 1), "duplicate": string(valid) + strings.Split(string(valid), "[[operations]]")[1]}
	cases["duplicate"] = string(valid) + "\n[[operations]]\n" + strings.Split(string(valid), "[[operations]]")[1]
	cases["toml"] = "version = ["
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			os.WriteFile(p, []byte(body), 0600)
			if _, err := Load(cfg, wd); err == nil {
				t.Fatal("invalid policy accepted")
			}
		})
	}
}
func TestReadOperationIsScopedPagedAndBounded(t *testing.T) {
	cfg, wd, root := t.TempDir(), t.TempDir(), t.TempDir()
	writePolicy(t, cfg, wd, root, "")
	os.WriteFile(filepath.Join(root, "a"), []byte("one\ntwo\nthree\n"), 0600)
	r, err := Call(context.Background(), cfg, wd, "source", json.RawMessage(`{"path":"a","offset":2,"limit":1}`))
	if err != nil || r.IsError {
		t.Fatal(string(r.Content), err)
	}
	var result struct {
		Lines      []string
		TotalLines int
	}
	if err = json.Unmarshal(r.Content, &result); err != nil {
		t.Fatal(err)
	}
	if result.TotalLines != 3 || len(result.Lines) != 1 || result.Lines[0] != "two" {
		t.Fatal(string(r.Content))
	}
	os.WriteFile(filepath.Join(root, "binary"), []byte{0, 1}, 0600)
	os.WriteFile(filepath.Join(root, "huge"), make([]byte, MaxFileBytes+1), 0600)
	for _, args := range []string{`{"path":"../secret"}`, `{"path":"` + filepath.ToSlash(filepath.Join(wd, "secret")) + `"}`, `{"path":"."}`, `{"path":"a","limit":1001}`, `{"path":"a","offset":0}`, `{"path":"a","limit":1.5}`, `{"path":"a","root":"/"}`, `{"path":"binary"}`, `{"path":"huge"}`, `null`, `{"path":"a"} {}`} {
		res, err := Call(context.Background(), cfg, wd, "source", json.RawMessage(args))
		if err == nil && !res.IsError {
			t.Errorf("accepted %s", args)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = Call(ctx, cfg, wd, "source", json.RawMessage(`{"path":"a"}`)); err != context.Canceled {
		t.Fatal(err)
	}
}
func TestReadOperationRejectsEscapingSymlink(t *testing.T) {
	cfg, wd, root := t.TempDir(), t.TempDir(), t.TempDir()
	writePolicy(t, cfg, wd, root, "")
	os.WriteFile(filepath.Join(wd, "secret"), []byte("PRIVATE"), 0600)
	if err := os.Symlink(wd, filepath.Join(root, "outside")); err != nil {
		t.Skip(err)
	}
	r, err := Call(context.Background(), cfg, wd, "source", json.RawMessage(`{"path":"outside/secret"}`))
	if err == nil && !r.IsError {
		t.Fatal("symlink escaped root")
	}
}

func TestReadOperationRefusesOversizedLineRangeWithoutSilentlyClipping(t *testing.T) {
	cfg, wd, root := t.TempDir(), t.TempDir(), t.TempDir()
	writePolicy(t, cfg, wd, root, "")
	os.WriteFile(filepath.Join(root, "large-line"), []byte(strings.Repeat("a", MaxResultBytes+1)), 0600)
	r, e := Call(context.Background(), cfg, wd, "source", json.RawMessage(`{"path":"large-line"}`))
	if e != nil || !r.IsError || !strings.Contains(string(r.Content), "64 KiB") {
		t.Fatal(string(r.Content), e)
	}
}
