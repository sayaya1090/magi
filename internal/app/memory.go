package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// memoryOf is one workdir's durable memory, and the state of the files it was read from.
//
// The stamp is what makes the cache safe to keep. Without it the cache was permanent: read once,
// held for the life of the process — so a person who edited AGENTS.md in a running session got the
// old text on every later turn, and nothing anywhere said why. The file is the one place a person
// writes standing instructions, and standing instructions that silently do not apply are worse than
// none: the belief that they are in force outlives the fact.
type memoryOf struct {
	text  string
	stamp string
}

// projectMemory loads durable memory (AGENTS.md) that is injected into every
// system prompt and never compacted away — a durable, project-scoped memory
// file. It reads, in order: global config AGENTS.md, project AGENTS.md, and
// project .magi/AGENTS.md.
//
// Cached per workdir and re-read when any of those files changes. The check is three stats, which
// is nothing next to the turn it precedes, and it is the difference between "edit the file and it
// takes effect" and "edit the file and wonder".
func (a *App) projectMemory(workdir string) string {
	a.memMu.Lock()
	defer a.memMu.Unlock()
	if a.memCache == nil {
		a.memCache = map[string]memoryOf{}
	}

	sources := memorySources(a.plat, workdir)
	stamp := memoryStamp(sources)
	if m, ok := a.memCache[workdir]; ok && m.stamp == stamp {
		return m.text
	}

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
	a.memCache[workdir] = memoryOf{text: m, stamp: stamp}
	return m
}

// memorySources is where durable memory comes from, in the order it is joined.
func memorySources(plat interface{ ConfigDir() string }, workdir string) []string {
	var sources []string
	if plat != nil {
		sources = append(sources, filepath.Join(plat.ConfigDir(), "AGENTS.md"))
	}
	return append(sources,
		filepath.Join(workdir, "AGENTS.md"),
		filepath.Join(workdir, ".magi", "AGENTS.md"),
	)
}

// memoryStamp is the identity of those files as a set: whether each exists, and its size and
// modification time.
//
// Not a hash of the contents — that would read every file on every turn to answer a question that
// is almost always "nothing changed". Size and mtime miss an edit that keeps the byte count and
// lands inside the filesystem's timestamp resolution; that is the same bound `make` has lived with,
// and it is a far smaller gap than never noticing at all.
//
// A file that appears or disappears changes the stamp too, so adding AGENTS.md to a workspace that
// had none takes effect on the next turn rather than the next start.
func memoryStamp(sources []string) string {
	var b strings.Builder
	for _, p := range sources {
		st, err := os.Stat(p)
		if err != nil {
			b.WriteString("-|")
			continue
		}
		fmt.Fprintf(&b, "%d@%d|", st.Size(), st.ModTime().UnixNano())
	}
	return b.String()
}
