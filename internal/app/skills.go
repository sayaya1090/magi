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
// directory format (<slug>/SKILL.md with a frontmatter description) — the layout
// Claude Code/OpenCode tooling and the bundled engram plugin produce, so skills
// learned there are usable here without conversion.
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
			data, err := os.ReadFile(filepath.Join(claudeDir, e.Name(), "SKILL.md"))
			if err != nil {
				continue
			}
			text := strings.TrimSpace(string(data))
			if text == "" {
				continue
			}
			seen[e.Name()] = true
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
					}
				}
			}
		}
	}
	return b.String()
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
