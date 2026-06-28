package app

import (
	"os"
	"path/filepath"
	"strings"
)

// projectMemory loads durable memory (AGENTS.md) that is injected into every
// system prompt and never compacted away — a durable, project-scoped memory
// file. It reads, in order: global config AGENTS.md, project AGENTS.md, and
// project .magi/AGENTS.md. Results are cached per workdir.
func (a *App) projectMemory(workdir string) string {
	a.memMu.Lock()
	defer a.memMu.Unlock()
	if a.memCache == nil {
		a.memCache = map[string]string{}
	}
	if m, ok := a.memCache[workdir]; ok {
		return m
	}

	var sources []string
	if a.plat != nil {
		sources = append(sources, filepath.Join(a.plat.ConfigDir(), "AGENTS.md"))
	}
	sources = append(sources,
		filepath.Join(workdir, "AGENTS.md"),
		filepath.Join(workdir, ".magi", "AGENTS.md"),
	)

	var b strings.Builder
	for _, p := range sources {
		data, err := os.ReadFile(p)
		if err != nil || len(strings.TrimSpace(string(data))) == 0 {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(strings.TrimRight(string(data), "\n"))
	}
	m := b.String()
	a.memCache[workdir] = m
	return m
}
