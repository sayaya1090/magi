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

// The reported failure mode: a bash-driven fix cycle (sed -i → build → test, repeat) had
// every fix invisible to the guard, so the third identical build/test call was hard-blocked
// as a no-progress repeat mid-progress. Exec bash commands are now exempt from the hard
// block ENTIRELY — build/test can be run in arbitrarily many ways, so exec is defined as
// "anything not in the closed inspect-only set", never an enumeration of build tools. The
// stall layer still owns genuine exec spins (sinceProgress keeps climbing).
func TestBashFixCycleNotBlocked(t *testing.T) {
	g := newRunGuard()
	build := json.RawMessage(`{"command":"go build ./..."}`)
	custom := json.RawMessage(`{"command":"./run_checks.sh --fast"}`) // arbitrary runner, no whitelist entry

	for i := 0; i < 6; i++ { // way past repeatLimit, NO mutation registered at all
		if block, n, _ := g.check("bash", build); block {
			t.Fatalf("iteration %d: exec repeat (build) must never hard-block, n=%d", i, n)
		}
		if block, n, _ := g.check("bash", custom); block {
			t.Fatalf("iteration %d: exec repeat (custom runner) must never hard-block, n=%d", i, n)
		}
	}
	if g.blocked != 0 {
		t.Errorf("exec repeats must not accrue blocked count, got %d", g.blocked)
	}
	// …but they still count as no-progress, so the stall layer can terminate a real spin.
	if g.sinceProgress == 0 {
		t.Error("exec repeats must still climb sinceProgress for the stall layer")
	}

	// Control 1: an inspect-only bash repeat (outcome cannot change) still hard-blocks.
	g2 := newRunGuard()
	insp := json.RawMessage(`{"command":"cat main.go"}`)
	for i := 0; i < repeatLimit; i++ {
		g2.check("bash", insp)
	}
	if block, _, _ := g2.check("bash", insp); !block {
		t.Error("an inspect-only repeat must still be blocked")
	}

	// Control 2: non-bash repeats (read, etc.) keep the block untouched.
	g3 := newRunGuard()
	rd := json.RawMessage(`{"file":"main.go"}`)
	for i := 0; i < repeatLimit; i++ {
		g3.check("read", rd)
	}
	if block, _, _ := g3.check("read", rd); !block {
		t.Error("a read repeat must still be blocked")
	}

	// A detected mutation still resets the window (stall accuracy), fingerprints re-key.
	g4 := newRunGuard()
	g4.check("bash", build)
	if !g4.noteBashWrite("sed -i 's/a/b/' main.go") {
		t.Fatal("sed -i must register as a file mutation")
	}
	if g4.sinceProgress != 0 {
		t.Error("a registered mutation must reset the no-progress window")
	}
	if _, n, _ := g4.check("bash", build); n != 1 {
		t.Errorf("post-mutation build must start a fresh fingerprint, got n=%d", n)
	}
}

// MAGI_GUARD_EXEC_EXEMPT=off restores the pre-exemption baseline for A/B isolation:
// identical exec repeats hard-block again, and redirect-less mutations stop bumping
// the epoch.
func TestExecExemptOff(t *testing.T) {
	t.Setenv("MAGI_GUARD_EXEC_EXEMPT", "off")
	g := newRunGuard()
	build := []byte(`{"command":"go build ./..."}`)
	for i := 0; i < repeatLimit; i++ {
		g.check("bash", build)
	}
	if block, _, _ := g.check("bash", build); !block {
		t.Error("with the exemption off, an identical exec repeat must hard-block")
	}
	if g.noteBashWrite("sed -i 's/a/b/' f.go") {
		t.Error("with the exemption off, a redirect-less mutation must not bump the epoch")
	}
	if !g.noteBashWrite("echo hi > f.txt") {
		t.Error("redirect writes must still bump the epoch (pre-exemption baseline)")
	}
}
