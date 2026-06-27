package builtin

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// TestLocalizeBench measures findcontext's "where to edit" accuracy against this
// repo's own git history: each non-merge commit gives a realistic issue-like
// query (its subject) and a gold set (the files it changed). We score recall@k
// and MRR of the first gold file in findcontext's ranking. It is a self-contained
// proxy for SWE-bench localization — Go-only and single-repo, so treat absolute
// numbers as relative signal, not external truth. Gated; run with:
//
//	MAGI_LOCALIZE_BENCH=1 go test ./internal/adapter/tool/builtin/ -run TestLocalizeBench -v
func TestLocalizeBench(t *testing.T) {
	if os.Getenv("MAGI_LOCALIZE_BENCH") == "" {
		t.Skip("set MAGI_LOCALIZE_BENCH=1 to run the localization benchmark")
	}
	root := repoRoot(t)

	cases := localizeCases(t, root, 80)
	if len(cases) < 10 {
		t.Fatalf("too few benchmark cases: %d", len(cases))
	}

	var hit1, hit5, hit10 int
	var mrrSum float64
	scored := 0
	for _, c := range cases {
		raw, _ := json.Marshal(findCtxArgs{Query: c.query, Limit: 10})
		res, err := FindContext{}.Execute(context.Background(), raw, port.ToolEnv{Workdir: root})
		if err != nil || res.IsError {
			continue
		}
		var ranked []rankedFile
		if json.Unmarshal(res.Content, &ranked) != nil {
			continue
		}
		scored++
		rank := 0 // 1-based rank of the first gold file, 0 if absent in top-10
		for i, r := range ranked {
			if c.gold[r.Path] {
				rank = i + 1
				break
			}
		}
		if rank == 1 {
			hit1++
		}
		if rank >= 1 && rank <= 5 {
			hit5++
		}
		if rank >= 1 && rank <= 10 {
			hit10++
		}
		if rank >= 1 {
			mrrSum += 1.0 / float64(rank)
		}
	}
	if scored == 0 {
		t.Fatal("no cases scored")
	}
	n := float64(scored)
	t.Logf("LOCALIZE BENCH  cases=%d", scored)
	t.Logf("  recall@1 = %.3f (%d/%d)", float64(hit1)/n, hit1, scored)
	t.Logf("  recall@5 = %.3f (%d/%d)", float64(hit5)/n, hit5, scored)
	t.Logf("  recall@10= %.3f (%d/%d)", float64(hit10)/n, hit10, scored)
	t.Logf("  MRR      = %.3f", mrrSum/n)
}

type localizeCase struct {
	query string
	gold  map[string]bool // workdir-relative paths changed by the commit
}

// localizeCases builds (query, gold-files) pairs from recent non-merge commits
// that touched a small number of source files (a broad refactor isn't a precise
// localization target). Gold files must still exist at HEAD (the corpus).
func localizeCases(t *testing.T, root string, max int) []localizeCase {
	out := git(t, root, "log", "--no-merges", "-n", "400", "--pretty=format:%H\t%s")
	var cases []localizeCase
	for _, line := range strings.Split(out, "\n") {
		sha, subj, ok := strings.Cut(line, "\t")
		if !ok || subj == "" {
			continue
		}
		files := changedSourceFiles(t, root, sha)
		if len(files) == 0 || len(files) > 5 { // skip empty and sweeping commits
			continue
		}
		gold := map[string]bool{}
		for _, f := range files {
			if fileExists(filepath.Join(root, f)) {
				gold[f] = true
			}
		}
		if len(gold) == 0 {
			continue
		}
		// Strip the conventional "area:" prefix so the query reads like an issue
		// ("wrap thinking blocks to width") rather than carrying the file hint.
		q := subj
		if _, rest, ok := strings.Cut(subj, ": "); ok && len(rest) > 8 {
			q = rest
		}
		cases = append(cases, localizeCase{query: q, gold: gold})
		if len(cases) >= max {
			break
		}
	}
	return cases
}

func changedSourceFiles(t *testing.T, root, sha string) []string {
	out := git(t, root, "show", "--name-only", "--pretty=format:", sha)
	var files []string
	for _, f := range strings.Split(strings.TrimSpace(out), "\n") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		ext := filepath.Ext(f)
		if ext != ".go" || strings.HasSuffix(f, "_test.go") {
			continue // non-test Go source only: the realistic edit target
		}
		files = append(files, filepath.ToSlash(f))
	}
	sort.Strings(files)
	return files
}

func repoRoot(t *testing.T) string {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for d := wd; ; {
		if fileExists(filepath.Join(d, "go.mod")) {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			t.Fatal("go.mod not found above " + wd)
		}
		d = parent
	}
}

func git(t *testing.T, root string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	b, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return string(b)
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
