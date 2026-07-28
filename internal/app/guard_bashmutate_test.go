package app

import (
	"encoding/json"
	"testing"
)

// mutatesFiles classifies redirect-less file-mutating commands; read/inspect/build/test
// commands stay out (build artifacts are derived state, not source progress).
func TestMutatesFiles(t *testing.T) {
	yes := []string{
		"sed -i 's/a/b/' main.go",
		"sed -i.bak 's/a/b/' main.go",
		"perl -i -pe 's/a/b/' main.go",
		"patch -p1 < fix.patch",
		"cp config.example config.yaml",
		"mv old.go new.go",
		"rm -rf build/",
		"mkdir -p out/sub",
		"touch marker",
		"git apply fix.patch",
		"git checkout -- main.go",
		"go mod tidy",
		"pip install -r requirements.txt",
		"npm install",
		"tar -xzf src.tgz",
		"tar czf out.tgz src/",
		"cd /app && sed -i 's/x/y/' f.go && go build ./...",
	}
	for _, c := range yes {
		if !mutatesFiles(c) {
			t.Errorf("mutatesFiles(%q) = false, want true", c)
		}
	}
	no := []string{
		"go build ./...",
		"go test ./...",
		"go vet ./...",
		"make",
		"pytest -x",
		"python check.py",
		"git status",
		"git diff",
		"git log --oneline",
		"npm ls",
		"sed 's/a/b/' main.go", // no -i: prints to stdout, mutates nothing
		"tar -tzf src.tgz",     // list, not extract
		"cat main.go",
		"grep -rn foo .",
		"ls -la",
	}
	for _, c := range no {
		if mutatesFiles(c) {
			t.Errorf("mutatesFiles(%q) = true, want false", c)
		}
	}
}

// The guard counts; it does not refuse. A repeated call the model can see in its own context is
// the model's to break, and measuring said so: the trials magi force-stopped produced no pass while
// the ones that ran to the external deadline produced 76. check() never returns a block — what a
// repeat past the limit still does is climb a counter that fires ONE advisory nudge.
func TestGuardCountsRepeatsButNeverRefuses(t *testing.T) {
	g := newRunGuard()
	build := json.RawMessage(`{"command":"go build ./..."}`)
	insp := json.RawMessage(`{"command":"cat main.go"}`)
	rd := json.RawMessage(`{"file":"main.go"}`)

	for i := 0; i < 6; i++ { // way past repeatLimit, NO mutation registered at all
		for _, c := range []struct {
			name string
			args json.RawMessage
		}{{"bash", build}, {"bash", insp}, {"read", rd}} {
			if block, n, _ := g.check(c.name, c.args); block {
				t.Fatalf("iteration %d: %s must never be refused, n=%d", i, c.name, n)
			}
		}
	}
	// The repeat is still SEEN — that is what the advisory nudge reads.
	if g.blocked == 0 {
		t.Error("a repeat past the limit must still be counted for the nudge")
	}
	if g.shouldNudge() != "blocked" {
		t.Error("a repeated no-progress action must still earn one advisory nudge")
	}
	// …and it still counts as no-progress for the stall window.
	if g.sinceProgress == 0 {
		t.Error("repeats must still climb sinceProgress")
	}

	// A detected mutation still resets the window (stall accuracy), fingerprints re-key.
	g4 := newRunGuard()
	g4.check("bash", build)
	if authored, _ := g4.noteBashWrite("sed -i 's/a/b/' main.go"); !authored {
		t.Fatal("sed -i must register as a file mutation")
	}
	if g4.sinceProgress != 0 {
		t.Error("a registered mutation must reset the no-progress window")
	}
	if _, n, _ := g4.check("bash", build); n != 1 {
		t.Errorf("post-mutation build must start a fresh fingerprint, got n=%d", n)
	}
}

// A redirect-less mutation (sed -i, patch, cp) bumps the epoch just like a redirect write: a
// bash-driven fix cycle produces files, and a guard that cannot see them reads real progress as
// a stall.
func TestBashMutationBumpsTheEpoch(t *testing.T) {
	g := newRunGuard()
	if authored, _ := g.noteBashWrite("sed -i 's/a/b/' f.go"); !authored {
		t.Error("a redirect-less mutation must bump the epoch")
	}
	if authored, _ := g.noteBashWrite("echo hi > f.txt"); !authored {
		t.Error("a redirect write must bump the epoch")
	}
	if authored, _ := g.noteBashWrite("grep -n x f.go"); authored {
		t.Error("a read-only command must not bump the epoch")
	}
}
