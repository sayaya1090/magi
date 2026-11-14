package app

import (
	"embed"
	"io/fs"
	"path"
	"strings"
)

// builtin_skills/<name>/SKILL.md ship inside the binary. Each names the agent kinds it is for in its
// frontmatter (clients: coding, word); a skill that names none is for the coding agent only, so a new
// file cannot reach office or mail companions by accident. A workspace skill of the same name wins.
//
//go:embed builtin_skills
var builtinSkillFS embed.FS

type builtinSkill struct {
	body        string
	description string
	clients     []ClientKind
}

// parseBuiltinSkill reads the description and clients lines of a SKILL.md frontmatter.
func parseBuiltinSkill(text string) builtinSkill {
	s := builtinSkill{body: text, description: frontmatterDescription(text)}
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return s
	}
	for _, l := range lines[1:] {
		t := strings.TrimSpace(l)
		if t == "---" {
			break
		}
		if v, ok := strings.CutPrefix(t, "clients:"); ok {
			for _, k := range strings.Split(v, ",") {
				if k = strings.TrimSpace(k); k != "" {
					s.clients = append(s.clients, ClientKind(k))
				}
			}
		}
	}
	return s
}

// builtinsFor is the part of [all] given to an agent of [kind].
func builtinsFor(kind ClientKind, all map[string]builtinSkill) map[string]builtinSkill {
	out := map[string]builtinSkill{}
	for name, s := range all {
		clients := s.clients
		if len(clients) == 0 {
			clients = []ClientKind{ClientCoding}
		}
		for _, k := range clients {
			if k == kind {
				out[name] = s
				break
			}
		}
	}
	return out
}

var builtinSkills = func() map[string]builtinSkill {
	out := map[string]builtinSkill{}
	entries, err := fs.ReadDir(builtinSkillFS, "builtin_skills")
	if err != nil {
		panic("built-in skills: " + err.Error())
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		b, err := builtinSkillFS.ReadFile(path.Join("builtin_skills", e.Name(), "SKILL.md"))
		if err != nil {
			panic("built-in skill " + e.Name() + ": " + err.Error())
		}
		out[e.Name()] = parseBuiltinSkill(string(b))
	}
	return out
}()
