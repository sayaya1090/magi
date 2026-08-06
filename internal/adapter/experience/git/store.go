// Package git implements a git-backed shared experience store (D13): a team's
// accumulated memories and skills live in a directory (typically a git repo the
// team commits/pulls). Retrieval is keyword-scored; contributions land in a
// review queue (pending/) and are committed best-effort.
package git

import (
	"context"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
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
func (s *Store) Retrieve(ctx context.Context, query string, agentGroups []string) ([]port.Memory, []port.Skill, error) {
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
		h, body := parseSkill(text)
		// Narrowed before scoring, not after: a skill this agent may not see must not take one of
		// the few slots the budget allows, or labelling a skill would cost the agents that cannot
		// use it the very context it was meant to save them.
		if !visibleTo(h.AgentGroups, agentGroups) {
			continue
		}
		skills = append(skills, scored[port.Skill]{
			score: overlap(terms, text),
			v:     port.Skill{Name: strings.TrimSuffix(filepath.Base(f), ".md"), Description: h.Description, Body: body},
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
	// One file per proposal was fine when a run was the unit and the store was read by people.
	// A resident process solves problems for weeks, and the same fact arrives again and again —
	// so an unbounded pile of near-identical memories is not a growing brain, it is retrieval
	// getting worse at its own expense. A fact already held is not written twice.
	existing := map[string]bool{}
	for _, f := range readDir(memDir) {
		existing[memoryKey(readFile(f))] = true
	}
	for _, m := range c.Memories {
		body := m.Text
		if len(m.Tags) > 0 {
			body = "tags: " + strings.Join(m.Tags, ", ") + "\n\n" + body
		}
		if c.Source != "" {
			body += "\n\n(source: " + c.Source + ")"
		}
		if existing[memoryKey(body)] {
			continue
		}
		existing[memoryKey(body)] = true
		// Named from the CONTENT, not the clock. `mem-<second>-<index>` collided: two proposals
		// in the same second with the same index wrote the same path, and the second silently
		// replaced the first. A run-per-task never noticed; a process that proposes all day would
		// lose facts it had just learned. A content name cannot collide with a different fact and
		// always collides with the same one, which is the behaviour wanted in both directions.
		name := filepath.Join(memDir, "mem-"+memoryID(body)+".md")
		if err := os.WriteFile(name, []byte(body), 0o644); err != nil {
			return err
		}
	}
	if len(c.Skills) > 0 {
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return err
		}
	}
	day := time.Now().UTC().Format("2006-01-02")
	for _, sk := range c.Skills {
		name := filepath.Join(skillDir, "skill-"+sanitize(sk.Name)+".md")
		// Learning the same lesson twice is different evidence from learning it once, and this
		// used to overwrite: the second observation erased the first and left nothing to tell
		// them apart. Carry the count and the dates forward instead.
		h := skillHeader{Description: sk.Description, FirstSeen: day, LastSeen: day, Observed: 1}
		if prev := readFile(name); prev != "" {
			old, _ := parseSkill(prev)
			h.Observed = old.Observed + 1
			if old.FirstSeen != "" {
				h.FirstSeen = old.FirstSeen
			}
			// Groups are a HUMAN's decision about who a skill is for. A re-learn carries none, and
			// dropping them would silently re-expose a skill somebody had narrowed.
			h.AgentGroups = old.AgentGroups
		}
		if err := os.WriteFile(name, []byte(renderHeader(h, sk.Body)), 0o644); err != nil {
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

// memoryID is a short stable name for a fact, derived from the same key that decides sameness.
func memoryID(text string) string {
	// fnv.Write is documented never to return an error, so there is nothing here to propagate.
	h := fnv.New64a()
	h.Write([]byte(memoryKey(text))) //nolint:errcheck // hash.Hash never errors
	return strconv.FormatUint(h.Sum64(), 36)
}

// memoryKey is what makes two memories the same one. The source line is stripped because WHERE a
// fact was learned does not change WHAT it says, and whitespace and case are folded because a
// re-learn is rarely byte-identical to the first telling.
func memoryKey(text string) string {
	if i := strings.LastIndex(text, "\n\n(source: "); i >= 0 {
		text = text[:i]
	}
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
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
