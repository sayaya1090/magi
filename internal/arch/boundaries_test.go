package arch

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// modPath is this module's import prefix; only imports under it are layered.
const modPath = "github.com/sayaya1090/magi/"

var importRe = regexp.MustCompile(`"` + regexp.QuoteMeta(modPath) + `([^"]+)"`)

// layerOf names the layer a repo-relative path belongs to. Longest prefix first.
func layerOf(p string) string {
	for _, l := range []string{"internal/core", "internal/port", "internal/app", "internal/adapter", "internal/eval", "cmd"} {
		if strings.HasPrefix(p, l) {
			return l
		}
	}
	return "" // shared leaves (jsonx, httpx, config, version, prompt, update) — unlayered on purpose
}

// goFiles walks the repo for production Go files, repo-relative.
func goFiles(t *testing.T) []string {
	t.Helper()
	root := filepath.Join("..", "..")
	var out []string
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "runs", "node_modules", "plugins", "bench", "scratchpad":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 100 {
		t.Fatalf("walked only %d production files — the walk is broken, so this checks nothing", len(out))
	}
	sort.Strings(out)
	return out
}

// imports returns the module-internal imports of one file, repo-relative.
func imports(t *testing.T, rel string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, m := range importRe.FindAllStringSubmatch(string(b), -1) {
		out = append(out, m[1])
	}
	return out
}

// The two layers that hold the domain must not know what is outside them. core is the model, port
// is the set of interfaces the outside implements; between them they are what every other layer
// depends ON, so an edge out of either is the one that turns a dependency graph into a cycle.
//
// Both are clean today — measured, not assumed — so this is a hard rule rather than a ratchet.
func TestTheDomainLayersDependOnNothingAboveThem(t *testing.T) {
	allowed := map[string][]string{
		"internal/core": {"internal/core"},
		"internal/port": {"internal/core", "internal/port"},
	}
	checked := 0
	for _, f := range goFiles(t) {
		want, ok := allowed[layerOf(f)]
		if !ok {
			continue
		}
		checked++
		for _, imp := range imports(t, f) {
			fine := false
			for _, a := range want {
				if strings.HasPrefix(imp, a) {
					fine = true
				}
			}
			if !fine {
				t.Errorf("%s imports %s — %s may import only %s",
					f, imp, layerOf(f), strings.Join(want, " and "))
			}
		}
	}
	if checked == 0 {
		t.Fatal("no core or port file was examined, so this asserts nothing")
	}
	t.Logf("%d core/port files checked", checked)
}

// appReachesIntoAdapters is the coupling that exists: six files in the application layer import
// the builtin tool package directly, for the tool-name sets and note helpers that live there.
// It is the wrong direction for this architecture — the application should know ports, and the
// adapters should know the application — and unwinding it is not a test's business.
//
// So it is frozen instead. These files may keep doing it; a seventh may not appear without someone
// deciding that it should, which is the whole value: the decision becomes visible instead of
// arriving inside an unrelated change.
var appReachesIntoAdapters = map[string]bool{
	"internal/app/background.go":   true,
	"internal/app/diagnose.go":     true,
	"internal/app/guard.go":        true,
	"internal/app/observed.go":     true,
	"internal/app/shellcmd.go":     true,
	"internal/app/tool_outcome.go": true,
}

func TestTheApplicationLayerDoesNotGrowNewAdapterImports(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range goFiles(t) {
		if layerOf(f) != "internal/app" {
			continue
		}
		for _, imp := range imports(t, f) {
			if !strings.HasPrefix(imp, "internal/adapter") {
				continue
			}
			seen[f] = true
			if !appReachesIntoAdapters[f] {
				t.Errorf("%s imports %s — the application layer reaching into an adapter is the "+
					"wrong direction here. If it is the right call anyway, add the file to "+
					"appReachesIntoAdapters and say why in the commit.", f, imp)
			}
		}
	}
	// And the list shrinks when the coupling does, so it cannot quietly become a list of files
	// that stopped importing anything.
	for f := range appReachesIntoAdapters {
		if !seen[f] {
			t.Errorf("%s no longer imports an adapter — drop it from appReachesIntoAdapters", f)
		}
	}
	if len(seen) == 0 {
		t.Fatal("no app file was examined, so this asserts nothing")
	}
}
