package app

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sayaya1090/magi/internal/port"
)

// loadSkills reads markdown skills for a workdir, from the global config dir and
// the project's .magi/skills. Each file: first line = description, full body
// follows. Results are cached per workdir. (skill capability)
func (a *App) loadSkills(workdir string) []port.Skill {
	a.memMu.Lock()
	defer a.memMu.Unlock()
	if a.skillCache == nil {
		a.skillCache = map[string][]port.Skill{}
	}
	if s, ok := a.skillCache[workdir]; ok {
		return s
	}

	var dirs []string
	if a.plat != nil {
		dirs = append(dirs, filepath.Join(a.plat.ConfigDir(), "skills"))
	}
	dirs = append(dirs, filepath.Join(workdir, ".magi", "skills"))

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
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	a.skillCache[workdir] = skills
	return skills
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
