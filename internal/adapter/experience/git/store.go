// Package git implements a git-backed shared experience store (D13): a team's
// accumulated memories and skills live in a directory (typically a git repo the
// team commits/pulls). Retrieval is keyword-scored; contributions land in a
// retrievable store and are committed best-effort.
package git

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/atomicfile"
	"github.com/sayaya1090/magi/internal/core/embed"
	"github.com/sayaya1090/magi/internal/port"
)

// Same is what decides whether a record already held says the thing a new one says. Optional: with
// no embedder, identity is normalised-string equality, which is what this store used for its whole
// life and which every near-duplicate walks straight past.
type Same interface {
	Available() bool
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// Store is a filesystem/git-backed ExperienceStore rooted at Dir with layout:
//
//	<dir>/memories/*.md   memories (the whole file is the retrievable text)
//	<dir>/skills/*.md     skills (first line = description)
//
// There is no pending/ — this comment advertised one for a year after the review gate went, and
// EXTENDING.md taught a promotion step nobody could ever perform. See Propose for why the gate is
// gone.
type Store struct {
	dir  string
	same Same
	// usageMu serializes THIS instance's read-modify-write of wiki/.usage. Instances are
	// sometimes ad hoc (New per call), so the cross-process and cross-instance halves are
	// covered by WikiTouch's merge-on-write, not by this lock.
	usageMu sync.Mutex
}

// New returns a store rooted at dir.
func New(dir string) *Store { return &Store{dir: dir} }

// WithSame attaches the judge of "is this already written down". Nil leaves identity textual.
func (s *Store) WithSame(j Same) *Store { s.same = j; return s }

// dupThreshold is the cosine above which two records are treated as the same thing.
//
// High on purpose. The cost of the two errors is not symmetric: a false MERGE loses a fact
// permanently and silently, while a false SPLIT leaves a near-duplicate that a person can see and
// that retrieval already tolerates. 0.93 admits rephrasings of one fact and rejects two facts
// about one topic — measured against the failure this exists for, which is the same lesson
// arriving in different words week after week. MAGI_DEDUP_COSINE overrides for a corpus whose
// embedding model scores differently; 0 disables semantic dedup and leaves identity textual.
func dupThreshold() float64 {
	if v := strings.TrimSpace(os.Getenv("MAGI_DEDUP_COSINE")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return 0.93
}

// alreadyHeld reports which of the candidate texts an existing record already says. It asks once,
// for every existing text and every candidate together, so proposing a batch costs one request
// and the existing texts come from the embedding cache after the first time.
//
// A silent no on any failure: dedup is an optimisation of the store's shape, and a record lost
// because an embedding endpoint was down is a worse outcome than one written twice.
func (s *Store) alreadyHeld(ctx context.Context, existing, candidates []string) []bool {
	held := make([]bool, len(candidates))
	th := dupThreshold()
	if s.same == nil || !s.same.Available() || th <= 0 || len(existing) == 0 || len(candidates) == 0 {
		return held
	}
	vecs, err := s.same.Embed(ctx, append(append([]string{}, existing...), candidates...))
	if err != nil || len(vecs) != len(existing)+len(candidates) {
		return held
	}
	for i := range candidates {
		cv := vecs[len(existing)+i]
		for j := range existing {
			if embed.Cosine(vecs[j], cv) >= th {
				held[i] = true
				break
			}
		}
	}
	return held
}

// Retrieve returns memories and skills whose text best matches the query
// (keyword overlap), capped to a few results. Secrets never live here by policy.
func (s *Store) Retrieve(ctx context.Context, query string, agentGroups []string) ([]port.Memory, []port.Skill, error) {
	mems, skills, err := s.Pool(ctx, query, agentGroups)
	if err != nil {
		return nil, nil, err
	}
	return topMemories(mems, 5), topSkills(skills, 3), nil
}

// Pool is every document this tier holds, each carrying its lexical score — the candidates a
// ranker may choose from, before any cap.
//
// It exists because a semantic re-rank cannot promote what lexical scoring already dropped.
// Retrieve discards a zero — a document sharing no word with the query — and that zero is exactly
// the case embeddings are for: asked about "billing", the note about invoices scores nothing, and
// by the time a vector could speak for it, it is gone. So the ranker gets everything and decides;
// Retrieve stays the lexical-only path, built from the same pool so there is one implementation of
// what scoring a tier means.
//
// Unbounded on purpose. The embedding client caches per document, so the steady-state cost of a
// large pool is one embedding for the query — the same trade the MCP search already makes — and a
// bound here would silently decide which documents can never be found, by file order.
func (s *Store) Pool(ctx context.Context, query string, agentGroups []string) ([]Scored[port.Memory], []Scored[port.Skill], error) {
	terms := tokenize(query)

	var mems []Scored[port.Memory]
	for _, f := range readDir(filepath.Join(s.dir, "memories")) {
		text := readFile(f)
		if text == "" {
			continue
		}
		mems = append(mems, Scored[port.Memory]{
			Score: overlap(terms, text),
			V:     port.Memory{ID: filepath.Base(f), Text: text},
		})
	}
	var skills []Scored[port.Skill]
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
		skills = append(skills, Scored[port.Skill]{
			Score: overlap(terms, text),
			V:     port.Skill{Name: strings.TrimSuffix(filepath.Base(f), ".md"), Description: h.Description, Body: body},
		})
	}

	return mems, skills, nil
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
	var existingText []string
	for _, f := range readDir(memDir) {
		t := readFile(f)
		existing[memoryKey(t)] = true
		if t != "" {
			existingText = append(existingText, t)
		}
	}
	// Bodies first, so the semantic question is asked ONCE for the whole batch rather than per
	// memory — and so the answer is about the text that would actually be written.
	bodies := make([]string, len(c.Memories))
	for i, m := range c.Memories {
		body := m.Text
		if len(m.Tags) > 0 {
			body = "tags: " + strings.Join(m.Tags, ", ") + "\n\n" + body
		}
		if c.Source != "" {
			body += "\n\n(source: " + c.Source + ")"
		}
		bodies[i] = body
	}
	// "Already written down" in the sense a person means it: the same lesson in different words is
	// the same lesson. String identity — normalise, compare — was the whole test, and it is the
	// one a rephrasing walks straight past, which is how a store fills with the same fact told
	// eight ways and gets worse at retrieval at its own expense.
	held := s.alreadyHeld(ctx, existingText, bodies)
	for i := range c.Memories {
		body := bodies[i]
		if existing[memoryKey(body)] || held[i] {
			continue
		}
		existing[memoryKey(body)] = true
		existingText = append(existingText, body)
		// Named from the CONTENT, not the clock. `mem-<second>-<index>` collided: two proposals
		// in the same second with the same index wrote the same path, and the second silently
		// replaced the first. A run-per-task never noticed; a process that proposes all day would
		// lose facts it had just learned. A content name cannot collide with a different fact and
		// always collides with the same one, which is the behaviour wanted in both directions.
		name := filepath.Join(memDir, "mem-"+memoryID(body)+".md")
		// Atomic: the GLOBAL tier is shared by every companion of one person, so two of them
		// learning at the same moment is ordinary — and a retrieval running alongside reads every
		// file in the directory. A truncate-then-write between those two is a skill read as empty.
		if err := atomicfile.Write(name, []byte(body), 0o644); err != nil {
			return err
		}
	}
	if len(c.Skills) > 0 {
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return err
		}
	}
	day := time.Now().UTC().Format("2006-01-02")
	// A skill's identity is its FILENAME, and that is how near-duplicates get in: the writer names
	// the technique, and "run-go-tests", "running-go-tests" and "go-test-workflow" are three files
	// for one skill. sanitize maps punctuation, never wording. So before taking a new name, ask
	// whether an existing skill already IS this one — and if so, write to that name instead, which
	// turns what would have been a third file into the second observation of the first.
	//
	// Only the name is redirected. The body still merges through the same path, so a human's edits
	// to the existing skill keep the treatment they always had.
	byName := map[string]string{} // sanitized name -> its text, for the semantic ask
	var skillNames, skillTexts []string
	for _, f := range readDir(skillDir) {
		t := readFile(f)
		if t == "" {
			continue
		}
		base := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), "skill-"), ".md")
		h, body := parseSkill(t)
		byName[base] = t
		skillNames = append(skillNames, base)
		skillTexts = append(skillTexts, h.Description+"\n"+body)
	}
	want := make([]string, len(c.Skills))
	for i, sk := range c.Skills {
		want[i] = sk.Description + "\n" + sk.Body
	}
	sameAs := s.sameSkill(ctx, skillNames, skillTexts, want)
	for i, sk := range c.Skills {
		named := sanitize(sk.Name)
		if _, isNew := byName[named]; !isNew && sameAs[i] != "" {
			named = sameAs[i] // this technique already has a name here; keep using it
		}
		name := filepath.Join(skillDir, "skill-"+named+".md")
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
		if err := atomicfile.Write(name, []byte(renderHeader(h, sk.Body)), 0o644); err != nil {
			return err
		}
	}
	// Wiki edits: canonical pages, updated in place. Editor is the contribution's Source — the
	// same provenance line every memory carries.
	for _, e := range c.Wiki {
		if err := s.wikiWrite(ctx, e, c.Source); err != nil {
			return err
		}
	}
	s.gitCommit(ctx, "magi: add experience ("+stamp+")")
	return nil
}

// sameSkill answers, for each proposed skill, the name of an existing skill that is the same
// technique — or "" for none. One request for the whole batch; existing texts come from the
// embedding cache after the first call.
//
// The threshold is the same one memories use and for the same reason: a false merge loses a
// technique silently, a false split leaves a visible near-duplicate. Any failure answers "no",
// because a skill written under a second name is recoverable and one merged away is not.
func (s *Store) sameSkill(ctx context.Context, names, texts, want []string) []string {
	out := make([]string, len(want))
	th := dupThreshold()
	if s.same == nil || !s.same.Available() || th <= 0 || len(names) == 0 || len(want) == 0 {
		return out
	}
	vecs, err := s.same.Embed(ctx, append(append([]string{}, texts...), want...))
	if err != nil || len(vecs) != len(texts)+len(want) {
		return out
	}
	for i := range want {
		wv := vecs[len(texts)+i]
		best, bestAt := th, -1
		for j := range names {
			if c := embed.Cosine(vecs[j], wv); c >= best {
				best, bestAt = c, j
			}
		}
		if bestAt >= 0 {
			out[i] = names[bestAt]
		}
	}
	return out
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

// Scored is one candidate and the lexical score it earned. Exported because the composing store
// ranks across tiers and needs the score to fuse with, not just the order this tier would have
// used.
type Scored[T any] struct {
	Score int
	V     T
}

func topMemories(xs []Scored[port.Memory], n int) []port.Memory {
	sort.SliceStable(xs, func(i, j int) bool { return xs[i].Score > xs[j].Score })
	var out []port.Memory
	for _, x := range xs {
		if x.Score == 0 {
			continue
		}
		out = append(out, x.V)
		if len(out) >= n {
			break
		}
	}
	return out
}

func topSkills(xs []Scored[port.Skill], n int) []port.Skill {
	sort.SliceStable(xs, func(i, j int) bool { return xs[i].Score > xs[j].Score })
	var out []port.Skill
	for _, x := range xs {
		if x.Score == 0 {
			continue
		}
		out = append(out, x.V)
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

// SkillInfo is one stored skill with the header a person governing the store needs.
//
// Retrieve answers "what is relevant to this query" and returns three; this answers "what is in
// here", which is a different question and the one a supervisor asks. It carries the header rather
// than only the description because the governance decisions are made on it: how often a lesson has
// been re-learned says whether it is settled, when it was last seen says whether it still applies,
// and the groups say who it reaches.
type SkillInfo struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"` // "skill" — a procedure; "memory" — a fact
	Description string `json:"description"`
	Body        string `json:"body"`
	// Groups gate a SKILL's visibility: an agent outside them never sees it. Tags decorate a
	// MEMORY and gate nothing. Two fields rather than one because that difference is the whole
	// reason a person reads this screen — a skill that reaches nobody and a memory labelled
	// "auth" are not the same situation, and one field would print them identically.
	Groups    []string `json:"groups,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	Observed  int      `json:"observed"`
	FirstSeen string   `json:"firstSeen,omitempty"`
	LastSeen  string   `json:"lastSeen,omitempty"`
}

// Inventory lists everything in this tier, unscored and unfiltered.
//
// No agent-group narrowing: that exists to keep a retrieval budget honest, and a person looking at
// what the store holds must see the entries they are deciding about — including the ones narrowed
// away from every agent, which are exactly the ones nobody would otherwise notice had gone stale.
func (s *Store) Inventory(ctx context.Context) ([]SkillInfo, error) {
	out := []SkillInfo{}
	for _, f := range readDir(filepath.Join(s.dir, "skills")) {
		text := readFile(f)
		if text == "" {
			continue
		}
		h, body := parseSkill(text)
		out = append(out, SkillInfo{
			Name: strings.TrimSuffix(filepath.Base(f), ".md"), Kind: "skill",
			Description: h.Description, Body: body, Groups: h.AgentGroups,
			Observed: h.Observed, FirstSeen: h.FirstSeen, LastSeen: h.LastSeen,
		})
	}
	// And the other half of the store. Memories are retrieved into prompts exactly as skills are,
	// and until now nothing outside the retrieval could see one — an inventory that showed half a
	// store would have a person governing the half that happens to be legible.
	for _, f := range readDir(filepath.Join(s.dir, "memories")) {
		text := readFile(f)
		if text == "" {
			continue
		}
		tags, body := parseMemory(text)
		out = append(out, SkillInfo{
			Name: strings.TrimSuffix(filepath.Base(f), ".md"), Kind: "memory",
			Description: firstLine(body), Body: body, Tags: tags,
			Observed: 1, // a fact is written once; there is no second observation to count
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// parseMemory splits the "tags: a, b" line Propose writes off the front of a fact.
func parseMemory(text string) ([]string, string) {
	line, rest, ok := strings.Cut(text, "\n")
	after, isTags := strings.CutPrefix(line, "tags:")
	if !ok || !isTags {
		return nil, strings.TrimSpace(text)
	}
	var tags []string
	for _, t := range strings.Split(after, ",") {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}
	return tags, strings.TrimSpace(rest)
}

// firstLine is the line a person scans, which for a fact is the fact.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// Forget removes one skill by name.
//
// A promoted rule that turned out to be wrong has to be removable, or the store only ever grows and
// a supervisor stops promoting for fear of it. The name is matched against what Inventory returned
// — never joined as a path from a caller — so nothing outside this tier's skills directory can be
// reached by asking for it.
func (s *Store) Forget(ctx context.Context, name string) error {
	// Both directories, one function: whichever kind the name turns out to be, the deletion has
	// the same rule and the same danger, and a second copy of it would be the one that forgot to
	// match against the listing instead of joining the caller's string to a path.
	for _, kind := range []string{"skills", "memories"} {
		for _, f := range readDir(filepath.Join(s.dir, kind)) {
			if strings.TrimSuffix(filepath.Base(f), ".md") != name {
				continue
			}
			if err := os.Remove(f); err != nil {
				return fmt.Errorf("experience: forgetting %s: %w", name, err)
			}
			s.gitCommit(ctx, "magi: forget "+strings.TrimSuffix(kind, "s")+" "+name)
			return nil
		}
	}
	return fmt.Errorf("experience: nothing named %q in %s", name, s.dir)
}
