package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sayaya1090/magi/internal/port"
)

// loadSkills reads markdown skills for a workdir from three sources: the global
// config dir and the project's .magi/skills (flat .md — first line = description,
// full body follows), plus the project's .claude/skills in the skill-creator
// directory format (<slug>/SKILL.md with a frontmatter description) — the standard
// skill-creator layout that Claude Code tooling and the bundled engram plugin
// produce, so skills learned there are usable here without conversion.
//
// Results are cached per workdir, keyed by the source dirs' mtime signature: a
// skill saved mid-session (engram writes one after a verified turn) appears at
// the next load instead of after a restart. (skill capability)
func (a *App) loadSkills(workdir string) []port.Skill {
	var dirs []string
	if a.plat != nil {
		dirs = append(dirs, filepath.Join(a.plat.ConfigDir(), "skills"))
	}
	dirs = append(dirs, filepath.Join(workdir, ".magi", "skills"))
	claudeDir := filepath.Join(workdir, ".claude", "skills")

	sig := dirSignature(append(dirs, claudeDir))
	a.memMu.Lock()
	if a.skillCache == nil {
		a.skillCache = map[string][]port.Skill{}
	}
	if c, ok := a.skillCache[workdir]; ok && a.skillCacheSig[workdir] == sig {
		a.memMu.Unlock()
		return c
	}
	a.memMu.Unlock()

	seen := map[string]bool{}
	var skills []port.Skill
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			name := strings.TrimSuffix(e.Name(), ".md")
			if seen[name] {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			text := strings.TrimSpace(string(data))
			if text == "" {
				continue
			}
			desc := text
			if i := strings.IndexByte(text, '\n'); i >= 0 {
				desc = strings.TrimSpace(text[:i])
			}
			seen[name] = true
			skills = append(skills, port.Skill{Name: name, Description: desc, Body: text})
		}
	}
	// skill-creator layout: .claude/skills/<slug>/SKILL.md, description in frontmatter.
	if entries, err := os.ReadDir(claudeDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() || seen[e.Name()] {
				continue
			}
			if strings.HasPrefix(e.Name(), ".") {
				continue // dot-dirs are not skills (e.g. .archive/ holds pruned skills)
			}
			data, err := os.ReadFile(filepath.Join(claudeDir, e.Name(), "SKILL.md"))
			if err != nil {
				continue
			}
			text := strings.TrimSpace(string(data))
			if text == "" {
				continue
			}
			seen[e.Name()] = true
			// A skill-creator skill may bundle auxiliary files (scripts/,
			// references/, assets/) that SKILL.md references by relative path.
			// The body alone strands those references — the model has no idea
			// where the skill lives — so append a resource manifest with the
			// absolute skill dir and its file list.
			if manifest := skillResources(filepath.Join(claudeDir, e.Name())); manifest != "" {
				text += "\n\n" + manifest
			}
			skills = append(skills, port.Skill{
				Name:        e.Name(),
				Description: frontmatterDescription(text),
				Body:        text,
			})
		}
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	a.memMu.Lock()
	if a.skillCacheSig == nil {
		a.skillCacheSig = map[string]string{}
	}
	a.skillCache[workdir] = skills
	a.skillCacheSig[workdir] = sig
	a.memMu.Unlock()
	return skills
}

// dirSignature fingerprints the skill source dirs by mtime + entry count, so the
// cache invalidates when a skill is added/changed (a dir's mtime changes when an
// entry is created/removed; SKILL.md edits are caught by the subdir mtimes too).
func dirSignature(dirs []string) string {
	var b strings.Builder
	for _, d := range dirs {
		if fi, err := os.Stat(d); err == nil {
			fmt.Fprintf(&b, "%s=%d;", d, fi.ModTime().UnixNano())
			if entries, err := os.ReadDir(d); err == nil {
				fmt.Fprintf(&b, "n%d;", len(entries))
				for _, e := range entries {
					if e.IsDir() {
						if sfi, err := os.Stat(filepath.Join(d, e.Name())); err == nil {
							fmt.Fprintf(&b, "%s=%d;", e.Name(), sfi.ModTime().UnixNano())
						}
						// An in-place SKILL.md edit bumps the FILE's mtime, not its
						// directory's — stat it too so edits invalidate the cache.
						if mfi, err := os.Stat(filepath.Join(d, e.Name(), "SKILL.md")); err == nil {
							fmt.Fprintf(&b, "m%d;", mfi.ModTime().UnixNano())
						}
					}
				}
			}
		}
	}
	return b.String()
}

// skillResources renders a skill directory's bundled files (everything except
// SKILL.md itself) as a manifest the model can act on: the absolute base dir
// plus relative paths, so "run scripts/setup.py" style references in the body
// resolve via the read/bash tools. Bounded, and empty for a SKILL.md-only skill.
func skillResources(dir string) string {
	const maxEntries = 50
	var files []string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // unreadable entries are simply omitted
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil || rel == "SKILL.md" {
			return nil
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if len(files) == 0 {
		return ""
	}
	sort.Strings(files)
	truncated := false
	if len(files) > maxEntries {
		files, truncated = files[:maxEntries], true
	}
	var b strings.Builder
	b.WriteString("## Bundled skill resources\n")
	b.WriteString("This skill ships auxiliary files under `" + dir + "` — when the instructions above reference a relative path, resolve it against that directory (read/run with the usual tools):\n")
	for _, f := range files {
		b.WriteString("- " + f + "\n")
	}
	if truncated {
		b.WriteString("- … (more files omitted; list the directory for the rest)\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// frontmatterDescription extracts `description:` from a leading YAML frontmatter
// block (the skill-creator trigger line); falls back to the first non-frontmatter
// content line.
func frontmatterDescription(text string) string {
	lines := strings.Split(text, "\n")
	inFront := false
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if i == 0 && t == "---" {
			inFront = true
			continue
		}
		if inFront {
			if t == "---" {
				inFront = false
				continue
			}
			if v, ok := strings.CutPrefix(t, "description:"); ok {
				return strings.Trim(strings.TrimSpace(v), `"`)
			}
			continue
		}
		if t != "" {
			return t
		}
	}
	return ""
}

// skillBody returns a named skill's full instructions.
func (a *App) skillBody(workdir, name string) (string, bool) {
	for _, s := range a.loadSkills(workdir) {
		if s.Name == name {
			return s.Body, true
		}
	}
	return "", false
}
