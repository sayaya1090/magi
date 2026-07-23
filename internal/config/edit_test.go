package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func writeFile(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Setting a routing key creates the [routing] section when absent and the value
// round-trips through Load.
func TestSetKeyCreatesSection(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "model = \"base\"\n")
	if err := SetKey(p, "routing", "explore", "fast"); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Routing["explore"] != "fast" {
		t.Errorf("routing.explore = %q, want fast; file:\n%s", c.Routing["explore"], read(t, p))
	}
	if c.Model != "base" {
		t.Errorf("existing model clobbered: %q", c.Model)
	}
}

// Replacing an existing key (active or commented) updates in place and preserves
// surrounding comments.
func TestSetKeyReplacesAndPreservesComments(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "# my config\n# model = \"old\"\n\n[routing]\nexplore = \"slow\"\ncoder = \"strong\"\n")
	if err := SetKey(p, "routing", "explore", "fast"); err != nil {
		t.Fatal(err)
	}
	if err := SetKey(p, "", "model", "qwen3"); err != nil {
		t.Fatal(err)
	}
	out := read(t, p)
	if !strings.Contains(out, "# my config") {
		t.Errorf("comment lost:\n%s", out)
	}
	c, _ := Load(dir)
	if c.Routing["explore"] != "fast" || c.Routing["coder"] != "strong" {
		t.Errorf("routing = %v\n%s", c.Routing, out)
	}
	if c.Model != "qwen3" {
		t.Errorf("model = %q (commented model should be replaced)\n%s", c.Model, out)
	}
}

// Empty value clears the key.
func TestSetKeyClears(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "[routing]\nexplore = \"fast\"\ncoder = \"strong\"\n")
	if err := SetKey(p, "routing", "explore", ""); err != nil {
		t.Fatal(err)
	}
	c, _ := Load(dir)
	if _, ok := c.Routing["explore"]; ok {
		t.Errorf("explore should be cleared: %v", c.Routing)
	}
	if c.Routing["coder"] != "strong" {
		t.Errorf("coder should remain: %v", c.Routing)
	}
}

// The commented default template can be edited and stays valid TOML.
func TestSetKeyOnDefaultTemplate(t *testing.T) {
	dir := t.TempDir()
	if err := WriteDefaultIfMissing(dir); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "config.toml")
	if err := SetKey(p, "routing", "planner", "fast"); err != nil {
		t.Fatal(err)
	}
	if err := SetKey(p, "", "model", "qwen3"); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatalf("edited template must stay valid TOML: %v\n%s", err, read(t, p))
	}
	if c.Routing["planner"] != "fast" || c.Model != "qwen3" {
		t.Errorf("template edits not applied: model=%q routing=%v", c.Model, c.Routing)
	}
}

func TestAppendListItem(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	// Absent file/key: created in the preamble.
	if err := AppendListItem(path, "allow", "webfetch(**)"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), `allow = ["webfetch(**)"]`) {
		t.Fatalf("created list wrong: %s", b)
	}

	// Existing single-line list: appended, comments elsewhere preserved.
	os.WriteFile(path, []byte("# keep me\nallow = [\"read\"]\n\n[routing]\ncoder = \"m\"\n"), 0o644)
	if err := AppendListItem(path, "allow", "bash(**)"); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	s := string(b)
	if !strings.Contains(s, `allow = ["read", "bash(**)"]`) || !strings.Contains(s, "# keep me") || !strings.Contains(s, "[routing]") {
		t.Fatalf("append mangled the file: %s", s)
	}

	// Duplicate: no-op.
	if err := AppendListItem(path, "allow", "bash(**)"); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(path)
	if string(b2) != s {
		t.Fatalf("duplicate append changed the file:\n%s", b2)
	}

	// Multi-line array: refused, untouched.
	ml := "allow = [\n  \"read\",\n]\n"
	os.WriteFile(path, []byte(ml), 0o644)
	if err := AppendListItem(path, "allow", "x"); err == nil {
		t.Fatal("expected an error on a multi-line array")
	}
	b3, _ := os.ReadFile(path)
	if string(b3) != ml {
		t.Fatalf("multi-line array was modified: %s", b3)
	}
}

// When a commented template default sits above the user's active key, SetKey must
// update the ACTIVE line and leave the comment inert — activating the comment
// would create a duplicate top-level key that fails the whole-file TOML parse.
func TestSetKeyPrefersActiveOverComment(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "# model = \"tmpl\"\nmodel = \"actual\"\n\n[routing]\ncoder = \"x\"\n")
	if err := SetKey(p, "", "model", "changed"); err != nil {
		t.Fatal(err)
	}
	got := read(t, p)
	active := 0
	for _, ln := range strings.Split(got, "\n") {
		tl := strings.TrimSpace(ln)
		if strings.HasPrefix(tl, "[") {
			break
		}
		if strings.HasPrefix(tl, "model") && strings.Contains(tl, "=") {
			active++
		}
	}
	if active != 1 {
		t.Fatalf("want exactly 1 active top-level model line, got %d:\n%s", active, got)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatalf("resulting config must still parse: %v\n%s", err, got)
	}
	if c.Model != "changed" {
		t.Fatalf("model = %q, want \"changed\"", c.Model)
	}
	if !strings.Contains(got, "# model = \"tmpl\"") {
		t.Fatalf("template comment should be preserved:\n%s", got)
	}
}

// With no active key, SetKey activates the commented template default in place
// (no duplicate, no stray insertion).
func TestSetKeyActivatesLoneComment(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "# model = \"tmpl\"\n\n[routing]\ncoder = \"x\"\n")
	if err := SetKey(p, "", "model", "chosen"); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Model != "chosen" {
		t.Fatalf("model = %q, want \"chosen\"", c.Model)
	}
	got := read(t, p)
	if strings.Contains(got, "# model") {
		t.Fatalf("the comment should have been activated, not left commented:\n%s", got)
	}
}

// withFileLock must give exclusive access to a target across concurrent holders —
// the cross-process serialization SetKey relies on (two magi instances sharing one
// config.toml). Exercised here via goroutines, but the guard is the O_EXCL sidecar
// lock, which serializes any holders on the machine, not just this process.
func TestWithFileLockSerializes(t *testing.T) {
	target := filepath.Join(t.TempDir(), "config.toml")
	var active, maxSeen int32
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = withFileLock(target, func() error {
				n := atomic.AddInt32(&active, 1)
				for {
					m := atomic.LoadInt32(&maxSeen)
					if n <= m || atomic.CompareAndSwapInt32(&maxSeen, m, n) {
						break
					}
				}
				time.Sleep(time.Millisecond) // widen the window an overlap would land in
				atomic.AddInt32(&active, -1)
				return nil
			})
		}()
	}
	wg.Wait()
	if maxSeen != 1 {
		t.Fatalf("withFileLock allowed %d concurrent holders; want strict mutual exclusion", maxSeen)
	}
}

// A stale lock left by a crashed writer must not wedge config writes forever:
// withFileLock reclaims it once older than the stale threshold. Simulated by
// pre-creating the lock file with an old mtime.
func TestWithFileLockReclaimsStale(t *testing.T) {
	target := filepath.Join(t.TempDir(), "config.toml")
	lock := target + ".lock"
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * time.Second)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	ran := false
	if err := withFileLock(target, func() error { ran = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("a stale lock should be reclaimed so the write still runs")
	}
}

// Concurrent SetKey calls to distinct keys must all land — no lost update, and the
// file stays parseable — guarding the locked read-modify-write.
func TestSetKeyConcurrentNoLostUpdate(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "[routing]\n")
	var wg sync.WaitGroup
	keys := []string{"explore", "planner", "coder", "council", "verifier", "reviewer"}
	for _, k := range keys {
		wg.Add(1)
		go func(k string) {
			defer wg.Done()
			if err := SetKey(p, "routing", k, "m-"+k); err != nil {
				t.Errorf("SetKey %s: %v", k, err)
			}
		}(k)
	}
	wg.Wait()
	c, err := Load(dir)
	if err != nil {
		t.Fatalf("config must stay parseable after concurrent writes: %v\n%s", err, read(t, p))
	}
	for _, k := range keys {
		if c.Routing[k] != "m-"+k {
			t.Errorf("lost update: routing.%s = %q, want %q\n%s", k, c.Routing[k], "m-"+k, read(t, p))
		}
	}
}

// Clearing removes only the active line and leaves a template comment intact.
func TestSetKeyClearLeavesComment(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "# model = \"tmpl\"\nmodel = \"actual\"\n")
	if err := SetKey(p, "", "model", ""); err != nil {
		t.Fatal(err)
	}
	got := read(t, p)
	if strings.Contains(got, "\nmodel = ") || strings.HasPrefix(got, "model = ") {
		t.Fatalf("active model line should be gone:\n%s", got)
	}
	if !strings.Contains(got, "# model = \"tmpl\"") {
		t.Fatalf("template comment should be preserved:\n%s", got)
	}
}
