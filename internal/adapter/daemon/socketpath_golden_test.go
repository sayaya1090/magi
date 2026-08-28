package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The socket name is a contract between two implementations. The daemon derives it here; the
// JetBrains plugin derives it again in Kotlin, in
// clients/jetbrains/plugin/core/src/main/kotlin/dev/sayaya/magi/ide/transport/SocketPath.kt,
// because it has to find the socket before it can ask anything.
//
// One character of disagreement and the symptom is "there is no daemon here" — which is false, and
// looks true. The Kotlin side has its own tests, and they hold only the Kotlin side: change
// shortHash or the path normalisation in Go and every one of them stays green while the plugin
// stops finding anything. This file is the other half.
//
// Two kinds of answer, kept apart on purpose.
//
// The pure functions are pinned as literals: they depend on nothing but their input, and a number
// written down is a number somebody has to look at when it moves. "/private/tmp/ws1" is here
// because its hash has the top bit set (15699900456404220863) — it is also the test of whether the
// port divides unsigned.
//
// Path resolution is NOT pinned as literals. It depends on the platform and the volume: whether
// /var and /tmp are symlinks, whether the filesystem folds case. Written down as constants it would
// fail meaninglessly on Linux CI or pass meaninglessly. So the test builds the awkward layouts,
// runs the REAL resolution, and writes what it got to a golden file the Kotlin tests read. Nobody
// imagines an expected value; when Go moves, the file moves, and the port fails.
//
// Regenerate deliberately, and say in the commit which way it moved:
//
//	MAGI_GOLDEN_UPDATE=1 go test ./internal/adapter/daemon/ -run Golden
const goldenFile = "../../../clients/jetbrains/plugin/core/src/test/resources/socketpath-golden.json"

type goldenCase struct {
	Name   string `json:"name"`
	Note   string `json:"note"`
	Input  string `json:"input"`            // $ROOT-relative, so a temp directory does not enter the file
	Real   string `json:"real,omitempty"`   // what EvalSymlinks answered
	Errors bool   `json:"errors,omitempty"` // …or that it refused
}

// WorkspaceKey is deliberately NOT recorded per case: it hashes an absolute path, and these cases
// live in a temp directory whose name is different every run. What the plugin depends on is the
// RELATION — two spellings of one directory are one key — and that is asserted below, in Go, where
// both paths are in hand.

type goldens struct {
	Platform  string       `json:"platform"`
	ShortHash [][2]string  `json:"shortHash"` // input, output
	Sanitize  [][2]string  `json:"sanitize"`
	Socket    [][3]string  `json:"socketPath"` // configDir, workdir, output
	Layout    []string     `json:"layout"`     // how to build the tree the cases run against
	Cases     []goldenCase `json:"cases"`
}

// The four ways a port of EvalSymlinks has actually gone wrong, each built as a real tree and
// answered by the real function. They are all ordinary macOS shapes: /var and /tmp are symlinks
// already, and Homebrew writes the fourth one by hand.
var symlinkLayout = []string{
	"mkdir casedir",                // ① case is not corrected, even on a case-folding volume
	"mkdir real",                   // ② a component that does not exist is an error, not a half-answer
	"symlink link -> real",         //    (link/nope)
	"mkdir real/x",                 // ③ a link inside an absolute target is resolved too
	"symlink hop -> $ROOT/real",    //    (entry → $ROOT/hop/x → $ROOT/real/x)
	"symlink entry -> $ROOT/hop/x", //
	"mkdir Cellar/x/bin",           // ④ a target starting with .. rewinds the RESOLVED dest
	"mkdir usr/local/bin",          //    (Homebrew's own shape)
	"symlink usr/local/bin/foo -> ../../../Cellar/x/bin",
	"mkdir b/c",
	"symlink alink -> $ROOT/b/c", // …and alink/.. is b, not evt4 and not b/c/..
}

func buildSymlinkTree(t *testing.T, root string) {
	t.Helper()
	mk := func(p string) {
		if err := os.MkdirAll(filepath.Join(root, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ln := func(target, name string) {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	mk("casedir")
	mk("real/x")
	ln("real", "link")
	ln(filepath.Join(root, "real"), "hop")
	ln(filepath.Join(root, "hop", "x"), "entry")
	mk("Cellar/x/bin")
	mk("usr/local/bin")
	ln("../../../Cellar/x/bin", "usr/local/bin/foo")
	mk("b/c")
	ln(filepath.Join(root, "b", "c"), "alink")
}

func TestSocketPathGoldens(t *testing.T) {
	got := goldens{
		Platform: runtime.GOOS,
		ShortHash: [][2]string{
			{"/private/tmp/ws1", shortHash("/private/tmp/ws1")}, // top bit set: the unsigned-division test
			{"/a", shortHash("/a")},
			{"/", shortHash("/")},
			{"/Users/sayaya/IdeaProjects/magi", shortHash("/Users/sayaya/IdeaProjects/magi")},
			{"/프로젝트/앱", shortHash("/프로젝트/앱")},
		},
		Sanitize: [][2]string{
			{"a😀b", sanitize("a😀b")}, // one rune, one dash — walking chars gives "a--b"
			{"앱", sanitize("앱")},
			{"my-repo_2", sanitize("my-repo_2")},
			{"a.b c", sanitize("a.b c")},
		},
		Socket: [][3]string{
			{"/tmp/mw1", "/private/tmp/ws1", SocketPath("/tmp/mw1", "/private/tmp/ws1")},
		},
		Layout: symlinkLayout,
	}

	root := t.TempDir()
	buildSymlinkTree(t, root)
	// The root itself may be a symlink (it is, on macOS: /var → /private/var). Every answer is
	// written relative to the RESOLVED root, so the file holds no temp path.
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	rel := func(p string) string {
		switch {
		case p == realRoot:
			return "$ROOT"
		case strings.HasPrefix(p, realRoot+string(filepath.Separator)):
			return "$ROOT/" + filepath.ToSlash(strings.TrimPrefix(p, realRoot+string(filepath.Separator)))
		case p == root:
			return "$ROOT"
		case strings.HasPrefix(p, root+string(filepath.Separator)):
			return "$ROOT/" + filepath.ToSlash(strings.TrimPrefix(p, root+string(filepath.Separator)))
		}
		return p
	}

	for _, c := range []struct{ name, note, path string }{
		{"case-as-written", "the case on disk is not corrected", "casedir"},
		{"case-as-typed", "…nor is the case as typed, even where the volume folds it", "CaseDir"},
		{"missing-component", "a component that does not exist is an error, not a half-resolved path", "link/nope"},
		{"link-inside-target", "a link inside an absolute target is resolved too — the walk restarts", "entry"},
		{"dotdot-target", "a target starting with .. rewinds the resolved destination", "usr/local/bin/foo"},
		{"dotdot-after-link", "…and a trailing .. rewinds the resolved dest, not the path as written", "alink/.."},
	} {
		// NOT filepath.Join: it cleans "alink/.." to "alink"'s parent before EvalSymlinks ever sees
		// the "..", which is the very thing that case exists to measure.
		in := root + "/" + c.path
		out := goldenCase{Name: c.name, Note: c.note, Input: "$ROOT/" + c.path}
		if real, err := filepath.EvalSymlinks(in); err != nil {
			out.Errors = true
		} else {
			out.Real = rel(real)
		}
		got.Cases = append(got.Cases, out)
	}

	// WorkspaceKey hashes an absolute path, so its answers here are tied to this temp directory and
	// cannot be pinned. What CAN be pinned is the relation the plugin depends on: two spellings of
	// one directory are one key, and two directories are two.
	same := WorkspaceKey(filepath.Join(root, "link")) == WorkspaceKey(filepath.Join(root, "real"))
	if !same {
		t.Error("a symlink and its target gave two keys — that is the daemon nobody can find")
	}
	if WorkspaceKey(filepath.Join(root, "real")) == WorkspaceKey(filepath.Join(root, "b")) {
		t.Error("two directories gave one key")
	}
	// …and the case answer above is what makes the third relation true or false, per platform.
	got.Cases = append(got.Cases, goldenCase{
		Name:  "case-two-spellings-one-key",
		Note:  "whether CaseDir and casedir are one companion — a fact about the volume, not the code",
		Input: "$ROOT/CaseDir vs $ROOT/casedir",
		Real:  map[bool]string{true: "same", false: "different"}[WorkspaceKey(filepath.Join(root, "CaseDir")) == WorkspaceKey(filepath.Join(root, "casedir"))],
	})

	body, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, '\n')

	if os.Getenv("MAGI_GOLDEN_UPDATE") != "" {
		if err := os.MkdirAll(filepath.Dir(goldenFile), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenFile, body, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("golden rewritten: %s", goldenFile)
		return
	}

	want, err := os.ReadFile(goldenFile)
	if err != nil {
		t.Fatalf("%v — write it with MAGI_GOLDEN_UPDATE=1 go test ./internal/adapter/daemon/ -run Golden", err)
	}
	var prev goldens
	if err := json.Unmarshal(want, &prev); err != nil {
		t.Fatal(err)
	}
	if prev.Platform != runtime.GOOS {
		t.Skipf("golden was taken on %s; path resolution is a fact about the platform", prev.Platform)
	}
	if string(want) != string(body) {
		t.Errorf("the socket name derivation moved. The JetBrains plugin derives it again in "+
			"clients/jetbrains/plugin/core/src/main/kotlin/dev/sayaya/magi/ide/transport/SocketPath.kt "+
			"and will stop finding the socket — a symptom that reads as \"no daemon here\". Update both, "+
			"then MAGI_GOLDEN_UPDATE=1 go test ./internal/adapter/daemon/ -run Golden.\n\nwas:\n%s\n\nnow:\n%s",
			want, body)
	}
}
