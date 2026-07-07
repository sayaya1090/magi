// Package plugin implements update management for Lua plugins that were installed
// as git checkouts. The git remote is the authoritative source — no extra manifest
// field or sidecar is needed — so discovery, update, and install all lean on git:
// a plugin dir with a .git is "managed" (updatable); a hand-dropped dir is left
// untouched. Update is a fast-forward pull only, so a locally-modified plugin is
// never force-overwritten.
package plugin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Managed is a discovered plugin and whether it can be updated.
type Managed struct {
	Name    string // manifest name if present, else the directory base
	Dir     string // absolute plugin directory
	Source  string // git origin URL ("" when not a git checkout)
	Ref     string // current branch/tag (best-effort)
	Version string // manifest version (best-effort)
	Git     bool   // true when Dir is a git checkout (updatable)
}

// Result reports what UpdateOne did for one plugin.
type Result struct {
	Name     string
	Updated  bool
	From, To string // short commit before/after (when git)
	Skipped  string // reason when not updated (e.g. "no git remote", "git not installed")
}

// gitTimeout bounds each git invocation so a hung network fetch can't wedge an
// interactive `-update`.
const gitTimeout = 60 * time.Second

// manifestMeta is the subset of plugin.toml we read for display.
type manifestMeta struct {
	Name    string `toml:"name"`
	Version string `toml:"version"`
}

// Discover scans each root's immediate subdirectories for a plugin.toml and
// reports them, marking git checkouts as updatable. Missing roots are skipped.
func Discover(roots []string) []Managed {
	var out []Managed
	seen := map[string]bool{}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			dir := filepath.Join(root, e.Name())
			if _, err := os.Stat(filepath.Join(dir, "plugin.toml")); err != nil {
				continue
			}
			abs, err := filepath.Abs(dir)
			if err != nil {
				abs = dir
			}
			if seen[abs] {
				continue // same plugin reachable via two roots — report once
			}
			seen[abs] = true
			m := Managed{Name: e.Name(), Dir: abs}
			if meta, ok := readManifest(abs); ok {
				if meta.Name != "" {
					m.Name = meta.Name
				}
				m.Version = meta.Version
			}
			if isGitRepo(abs) {
				m.Git = true
				m.Source = gitRemote(abs)
				m.Ref = gitRef(abs)
			}
			out = append(out, m)
		}
	}
	return out
}

// UpdateOne fast-forwards a git-managed plugin to its remote. Non-git or
// remote-less plugins are reported as skipped, never mutated. A non-fast-forward
// (local commits/changes) is an error, not a forced reset.
func UpdateOne(ctx context.Context, m Managed) Result {
	if !m.Git {
		return Result{Name: m.Name, Skipped: "not a git checkout (manually managed)"}
	}
	if _, err := exec.LookPath("git"); err != nil {
		return Result{Name: m.Name, Skipped: "git not installed"}
	}
	if m.Source == "" {
		return Result{Name: m.Name, Skipped: "no git remote"}
	}
	before := gitRev(ctx, m.Dir)
	if _, err := git(ctx, m.Dir, "fetch", "--quiet"); err != nil {
		return Result{Name: m.Name, Skipped: "fetch failed: " + oneLineErr(err)}
	}
	// A detached HEAD or a branch with no configured upstream has no @{u} to
	// merge; report that precisely instead of blaming "local changes".
	if _, err := git(ctx, m.Dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); err != nil {
		return Result{Name: m.Name, Skipped: "no upstream tracking branch (detached or unset) — update skipped"}
	}
	if _, err := git(ctx, m.Dir, "merge", "--ff-only", "--quiet", "@{u}"); err != nil {
		return Result{Name: m.Name, Skipped: "not fast-forwardable (local commits) — update skipped"}
	}
	after := gitRev(ctx, m.Dir)
	if before == after {
		return Result{Name: m.Name, Updated: false, From: before, To: after}
	}
	return Result{Name: m.Name, Updated: true, From: before, To: after}
}

// Install clones url into destRoot/<name> (optionally at ref) and returns the
// resulting Managed. It refuses to overwrite an existing directory.
func Install(ctx context.Context, url, ref, destRoot string) (Managed, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return Managed{}, fmt.Errorf("git not installed")
	}
	name := repoName(url)
	if name == "" {
		return Managed{}, fmt.Errorf("cannot derive plugin name from %q", url)
	}
	if err := os.MkdirAll(destRoot, 0o755); err != nil {
		return Managed{}, err
	}
	dest := filepath.Join(destRoot, name)
	if _, err := os.Stat(dest); err == nil {
		return Managed{}, fmt.Errorf("plugin %q already exists at %s (use -update-plugins to update)", name, dest)
	}
	// No pin: a shallow clone of the default branch is enough. With a pin, clone in
	// full and `checkout` it — `clone --branch` accepts only a branch/tag, so a
	// commit SHA needs the object history present. Clean up a partial checkout on
	// any failure (a git timeout can SIGKILL mid-clone and leave dest behind,
	// which would otherwise wedge this plugin name against re-install).
	if ref == "" {
		if _, err := git(ctx, destRoot, "clone", "--depth", "1", url, dest); err != nil {
			_ = os.RemoveAll(dest)
			return Managed{}, fmt.Errorf("git clone: %w", err)
		}
	} else {
		if _, err := git(ctx, destRoot, "clone", url, dest); err != nil {
			_ = os.RemoveAll(dest)
			return Managed{}, fmt.Errorf("git clone: %w", err)
		}
		if _, err := git(ctx, dest, "checkout", "--quiet", ref); err != nil {
			_ = os.RemoveAll(dest)
			return Managed{}, fmt.Errorf("git checkout %q: %w", ref, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dest, "plugin.toml")); err != nil {
		_ = os.RemoveAll(dest)
		return Managed{}, fmt.Errorf("cloned repo has no plugin.toml (not a magi plugin)")
	}
	abs, _ := filepath.Abs(dest)
	m := Managed{Name: name, Dir: abs, Git: true, Source: gitRemote(abs), Ref: gitRef(abs)}
	if meta, ok := readManifest(abs); ok {
		if meta.Name != "" {
			m.Name = meta.Name
		}
		m.Version = meta.Version
	}
	return m, nil
}

// --- git helpers ---

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func isGitRepo(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && (info.IsDir() || info.Mode().IsRegular()) // dir, or a gitlink file (submodule/worktree)
}

func gitRemote(dir string) string {
	out, err := git(context.Background(), dir, "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func gitRef(dir string) string {
	out, err := git(context.Background(), dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func gitRev(ctx context.Context, dir string) string {
	out, err := git(ctx, dir, "rev-parse", "--short", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func readManifest(dir string) (manifestMeta, bool) {
	b, err := os.ReadFile(filepath.Join(dir, "plugin.toml"))
	if err != nil {
		return manifestMeta{}, false
	}
	var m manifestMeta
	if err := toml.Unmarshal(b, &m); err != nil {
		return manifestMeta{}, false
	}
	return m, true
}

// repoName derives the plugin directory name from a git URL: the last path
// segment with any ".git" suffix and trailing slash stripped.
func repoName(url string) string {
	s := strings.TrimSpace(url)
	if i := strings.IndexAny(s, "?#"); i >= 0 { // drop query/fragment first
		s = s[:i]
	}
	s = strings.TrimRight(s, "/")
	s = strings.TrimSuffix(s, ".git")
	if i := strings.LastIndexAny(s, "/:"); i >= 0 {
		s = s[i+1:]
	}
	return s
}

func oneLineErr(err error) string {
	return strings.TrimSpace(strings.ReplaceAll(err.Error(), "\n", " "))
}
