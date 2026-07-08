// Package git implements a git-backed shared experience store (D13): a team's
// accumulated memories and skills live in a directory (typically a git repo the
// team commits/pulls). Retrieval is keyword-scored; contributions land in a
// review queue (pending/) and are committed best-effort.
package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

// Store is a filesystem/git-backed ExperienceStore rooted at Dir with layout:
//
//	<dir>/memories/*.md   approved memories (retrievable)
//	<dir>/skills/*.md     approved skills (first line = description)
//	<dir>/pending/*.md    proposed contributions awaiting review
type Store struct {
	dir string
}

// New returns a store rooted at dir.
func New(dir string) *Store { return &Store{dir: dir} }

// Retrieve returns memories and skills whose text best matches the query
// (keyword overlap), capped to a few results. Secrets never live here by policy.
func (s *Store) Retrieve(ctx context.Context, query string) ([]port.Memory, []port.Skill, error) {
	terms := tokenize(query)

	var mems []scored[port.Memory]
	for _, f := range readDir(filepath.Join(s.dir, "memories")) {
		text := readFile(f)
		if text == "" {
			continue
		}
		mems = append(mems, scored[port.Memory]{
			score: overlap(terms, text),
			v:     port.Memory{ID: filepath.Base(f), Text: text},
		})
	}
	var skills []scored[port.Skill]
	for _, f := range readDir(filepath.Join(s.dir, "skills")) {
		text := readFile(f)
		if text == "" {
			continue
		}
		desc, body := splitFirstLine(text)
		skills = append(skills, scored[port.Skill]{
			score: overlap(terms, text),
			v:     port.Skill{Name: strings.TrimSuffix(filepath.Base(f), ".md"), Description: desc, Body: body},
		})
	}

	return topMemories(mems, 5), topSkills(skills, 3), nil
}

// Propose writes a contribution directly into the retrievable store and commits
// it best-effort. Memories land in memories/ and skills in skills/ — the same
// directories Retrieve reads — so a remembered fact is recallable immediately.
// (There is deliberately no pending/ review gate on the agent path: an autonomous
// run that can write but never read back its own learnings is write-only and
// useless. Provenance stays in the file body via tags and a (source: …) line.)
func (s *Store) Propose(ctx context.Context, c port.Contribution) error {
	memDir := filepath.Join(s.dir, "memories")
	skillDir := filepath.Join(s.dir, "skills")
	stamp := time.Now().UTC().Format("20060102-150405")
	if len(c.Memories) > 0 {
		if err := os.MkdirAll(memDir, 0o755); err != nil {
			return err
		}
	}
	for i, m := range c.Memories {
		name := filepath.Join(memDir, "mem-"+stamp+"-"+itoa(i)+".md")
		body := m.Text
		if len(m.Tags) > 0 {
			body = "tags: " + strings.Join(m.Tags, ", ") + "\n\n" + body
		}
		if c.Source != "" {
			body += "\n\n(source: " + c.Source + ")"
		}
		if err := os.WriteFile(name, []byte(body), 0o644); err != nil {
			return err
		}
	}
	if len(c.Skills) > 0 {
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return err
		}
	}
	for _, sk := range c.Skills {
		name := filepath.Join(skillDir, "skill-"+sanitize(sk.Name)+".md")
		if err := os.WriteFile(name, []byte(sk.Description+"\n\n"+sk.Body), 0o644); err != nil {
			return err
		}
	}
	s.gitCommit(ctx, "magi: add experience ("+stamp+")")
	return nil
}

// gitCommit best-effort versions the store if it is a git repo.
func (s *Store) gitCommit(ctx context.Context, msg string) {
	if _, err := os.Stat(filepath.Join(s.dir, ".git")); err != nil {
		return
	}
	_ = exec.CommandContext(ctx, "git", "-C", s.dir, "add", ".").Run()
	_ = exec.CommandContext(ctx, "git", "-C", s.dir, "commit", "-m", msg).Run()
}

// ---- helpers ----

type scored[T any] struct {
	score int
	v     T
}

func topMemories(xs []scored[port.Memory], n int) []port.Memory {
	sort.SliceStable(xs, func(i, j int) bool { return xs[i].score > xs[j].score })
	var out []port.Memory
	for _, x := range xs {
		if x.score == 0 {
			continue
		}
		out = append(out, x.v)
		if len(out) >= n {
			break
		}
	}
	return out
}

func topSkills(xs []scored[port.Skill], n int) []port.Skill {
	sort.SliceStable(xs, func(i, j int) bool { return xs[i].score > xs[j].score })
	var out []port.Skill
	for _, x := range xs {
		if x.score == 0 {
			continue
		}
		out = append(out, x.v)
		if len(out) >= n {
			break
		}
	}
	return out
}

func readDir(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

func readFile(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func tokenize(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		if len(w) >= 3 {
			out[w] = true
		}
	}
	return out
}

func overlap(terms map[string]bool, text string) int {
	if len(terms) == 0 {
		return 0
	}
	lower := strings.ToLower(text)
	n := 0
	for t := range terms {
		if strings.Contains(lower, t) {
			n++
		}
	}
	return n
}

func splitFirstLine(s string) (string, string) {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:])
	}
	return s, ""
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, s)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
