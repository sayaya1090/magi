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
	Platform string `json:"platform"`
	// Placeholders says what the two tokens mean, in the file, because they mean different things
	// and reading one as the other is exactly how the two sides drifted apart the first time.
	Placeholders map[string]string `json:"placeholders"`
	// Regenerate is the sentence a failing test should print, on EITHER side. The Go side says
	// "the derivation moved, fix the Kotlin too"; the Kotlin side reads this so a plugin developer
	// gets the same next step instead of only "does not match".
	Regenerate string `json:"regenerate"`
	// CaseFolds: whether two spellings of one directory are one companion here. A fact about the
	// volume, not about the code — its own field rather than a row among the cases, which needed
	// the reader to branch on a case NAME and had a different grammar in every column.
	CaseFolds bool         `json:"caseFolds"`
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

// buildSymlinkTree builds the tree BY READING symlinkLayout — the same lines the golden file ships
// to the Kotlin side, interpreted by the same two rules.
//
// It used to be a second, hand-written builder standing beside the list, which is the mistake the
// whole file exists to prevent: one word counted in two places is a word two places disagree about.
// They had already drifted. The hand-written one pointed hop at the UNRESOLVED temp root while the
// Kotlin side substitutes $ROOT with its resolved form, so the port was walking a chain one hop
// shorter than the one Go measured — in the very case that measures how far the walk restarts.
//
// $ROOT is the temp directory AS CREATED, which on macOS is itself behind /var → /private/var. Not
// its resolved form: the longer chain is the one worth testing, and the resolved form has its own
// placeholder ($REAL) for the answers.
func buildSymlinkTree(t *testing.T, root string, layout []string) {
	t.Helper()
	for _, line := range layout {
		body, _, _ := strings.Cut(line, "//") // the list carries its reasons inline
		body = strings.TrimSpace(body)
		switch {
		case body == "":
		case strings.HasPrefix(body, "mkdir "):
			dir := strings.TrimSpace(strings.TrimPrefix(body, "mkdir "))
			if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o755); err != nil {
				t.Fatal(err)
			}
		case strings.HasPrefix(body, "symlink "):
			name, target, ok := strings.Cut(strings.TrimPrefix(body, "symlink "), " -> ")
			if !ok {
				t.Fatalf("layout line has no arrow: %q", line)
			}
			target = strings.ReplaceAll(strings.TrimSpace(target), "$ROOT", root)
			at := filepath.Join(root, filepath.FromSlash(strings.TrimSpace(name)))
			if err := os.Symlink(filepath.FromSlash(target), at); err != nil {
				t.Fatal(err)
			}
		default:
			// The Kotlin side refuses an unknown line too. A layout instruction nobody executes is
			// a case that silently stops being tested.
			t.Fatalf("layout line is neither mkdir nor symlink: %q", line)
		}
	}
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
		Placeholders: map[string]string{
			"$ROOT": "the temp directory as created, which may itself be a symlink — build the layout under this, " +
				"and read the inputs as written against it",
			"$REAL": "that directory after resolution (Go: filepath.EvalSymlinks, JVM: toRealPath) — every " +
				"answer below is relative to this",
		},
		Regenerate: "The socket name derivation moved. Both sides derive it — internal/adapter/daemon/daemon.go " +
			"(WorkspaceKey, sanitize, shortHash) and clients/jetbrains/plugin/core/src/main/kotlin/dev/sayaya/" +
			"magi/ide/transport/SocketPath.kt — and a difference of one character reads as \"there is no daemon " +
			"here\". Fix both, then regenerate: MAGI_GOLDEN_UPDATE=1 go test ./internal/adapter/daemon/ -run Golden",
	}

	root := t.TempDir()
	buildSymlinkTree(t, root, symlinkLayout)
	// The root itself may be a symlink (it is, on macOS: /var → /private/var). Every answer is
	// written relative to the RESOLVED root, so the file holds no temp path.
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	rel := func(p string) string {
		switch {
		case p == realRoot:
			return "$REAL"
		case strings.HasPrefix(p, realRoot+string(filepath.Separator)):
			return "$REAL/" + filepath.ToSlash(strings.TrimPrefix(p, realRoot+string(filepath.Separator)))
		case p == root:
			return "$REAL"
		case strings.HasPrefix(p, root+string(filepath.Separator)):
			return "$REAL/" + filepath.ToSlash(strings.TrimPrefix(p, root+string(filepath.Separator)))
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
	// Whether the two spellings are one companion is a fact about the VOLUME, not about the code, so
	// it is recorded as one — its own field, not a row among the cases. As a row it needed the
	// reader to branch on a case name (which goes quiet the day the name is edited) and it filled
	// the same columns with a different grammar.
	got.CaseFolds = WorkspaceKey(filepath.Join(root, "CaseDir")) == WorkspaceKey(filepath.Join(root, "casedir"))

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
		t.Errorf("%s\n\nwas:\n%s\n\nnow:\n%s", got.Regenerate, want, body)
	}
}
