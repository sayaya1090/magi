// Package builtin provides the core file/search tools as pure-Go implementations
// (no POSIX shell dependency), so they behave identically across macOS, Linux,
// and Windows. All tools are jailed to the session working directory.
package builtin

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// resolvePath joins p against workdir and verifies the result stays inside the
// workdir tree. It returns the cleaned absolute path, or an error if the path
// escapes the jail (F-TOOL common rule C2).
func resolvePath(workdir, p string) (string, error) {
	base := filepath.Clean(workdir)

	var abs string
	if filepath.IsAbs(p) {
		abs = filepath.Clean(p)
	} else {
		abs = filepath.Clean(filepath.Join(base, p))
	}

	// abs must equal base or be within base/.
	rel, err := filepath.Rel(base, abs)
	if err != nil {
		return "", fmt.Errorf("outside workdir: %s", p)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("outside workdir: %s", p)
	}
	return abs, nil
}

// findByBase searches the workdir tree for files whose base name matches the
// requested path's base name (case-insensitive), returning workdir-relative
// paths. It lets tools recover from imprecise paths (e.g. an agent asks for
// "DESIGN.md" when the file is "docs/DESIGN.md") instead of dead-ending on a
// bare "not found". Noise dirs are skipped and results are capped.
func findByBase(workdir, requested string) []string {
	want := strings.ToLower(filepath.Base(requested))
	if want == "" || want == "." {
		return nil
	}
	var hits []string
	root := filepath.Clean(workdir)
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "dist", "build", ".idea", ".cache":
				return fs.SkipDir
			}
			return nil
		}
		if strings.ToLower(d.Name()) == want {
			if rel, e := filepath.Rel(root, p); e == nil {
				hits = append(hits, rel)
			}
		}
		if len(hits) >= 8 {
			return fs.SkipAll
		}
		return nil
	})
	return hits
}

// resolveOrSuggest resolves p; if it doesn't exist as a file, it looks for
// same-named files in the tree. On a single match it returns that match's
// absolute path with locate=true and the relative match for noting; on multiple
// matches it returns a suggestion error string. (found=false, suggest!="" → tell
// the model; found=false, suggest=="" → genuinely missing.)
func resolveOrSuggest(workdir, p string) (abs string, locatedRel string, suggest string) {
	a, err := resolvePath(workdir, p)
	if err != nil {
		return "", "", ""
	}
	if fi, e := os.Stat(a); e == nil && !fi.IsDir() {
		return a, "", ""
	}
	hits := findByBase(workdir, p)
	switch len(hits) {
	case 0:
		return "", "", ""
	case 1:
		full := filepath.Join(filepath.Clean(workdir), hits[0])
		return full, hits[0], ""
	default:
		return "", "", "did you mean: " + strings.Join(hits, ", ")
	}
}
