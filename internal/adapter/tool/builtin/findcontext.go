package builtin

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// FindContext ranks workspace files by relevance to a natural-language query. It
// weights, in order: a file that DEFINES a queried symbol (def/func/class/type…),
// the file/path name, then plain content mentions and how many distinct query
// terms a file covers. This points the agent at where to EDIT, not just where a
// word appears — the #1 lever for resolving a coding task. Read-only.
type FindContext struct{}

type findCtxArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

type rankedFile struct {
	Path    string `json:"path"`
	Score   int    `json:"score"`
	Line    int    `json:"line,omitempty"` // 1-based line of the best symbol definition, if any
	Symbol  string `json:"symbol,omitempty"`
	Snippet string `json:"snippet,omitempty"`
}

func (FindContext) Name() string { return "findcontext" }
func (FindContext) Description() string {
	return "Rank the most relevant files for a natural-language query, prioritizing files that DEFINE the named symbols (functions/classes/types) — results include the symbol's name and 1-based definition line so you can read/edit there directly — then filename and content matches. Use to locate where to edit in a large codebase."
}
func (FindContext) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"integer"}},"required":["query"]}`)
}

var skipDirs = map[string]bool{".git": true, "node_modules": true, "vendor": true, "dist": true, ".magi": true, "build": true, "target": true, "__pycache__": true}

// defLineRE marks a line that DECLARES something, across common languages.
var defLineRE = regexp.MustCompile(`(?i)^\s*(?:export\s+|public\s+|private\s+|protected\s+|static\s+|async\s+|pub\s+)*(?:def|func|fn|function|class|type|interface|struct|impl|trait|enum|module|package|var|let|const)\b`)

// defNameRE captures the DECLARED IDENTIFIER from a definition line — the symbol
// name itself, not just that a def keyword is present. The optional (\([^)]*\))
// skips a Go method receiver (`func (r *T) Name(`). Covers def/func/fn/function/
// class/type/interface/struct/impl/trait/enum across common languages.
var defNameRE = regexp.MustCompile(`(?i)\b(?:def|func|fn|function|class|type|interface|struct|impl|trait|enum)\b\s*(?:\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)`)

// varDefRE captures top-level value bindings (`var Name`, `const NAME`, `let x`,
// `val y`) — useful for JS/TS/Kotlin where logic hangs off named consts.
var varDefRE = regexp.MustCompile(`(?i)^\s*(?:export\s+)?(?:var|let|const|val)\s+([A-Za-z_][A-Za-z0-9_]*)`)

// defNames extracts declared symbol name(s) from a single line (usually 0 or 1).
func defNames(line string) []string {
	var out []string
	if m := defNameRE.FindStringSubmatch(line); m != nil {
		out = append(out, m[1])
	}
	if m := varDefRE.FindStringSubmatch(line); m != nil {
		out = append(out, m[1])
	}
	return out
}

func (FindContext) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var a findCtxArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	terms := keywords(a.Query)
	if len(terms) == 0 {
		return errResult("", "query has no usable keywords"), nil
	}
	limit := a.Limit
	if limit <= 0 {
		limit = 10
	}

	root := filepath.Clean(env.Workdir)
	var ranked []rankedFile
	walked := 0
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if (strings.HasPrefix(d.Name(), ".") && p != root) || skipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if symlinkEscapesJail(env.Workdir, p, d) {
			return nil // an in-workdir symlink pointing outside would leak its snippet
		}
		if walked > 5000 {
			return fs.SkipDir
		}
		walked++
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)

		score := 0
		base := strings.ToLower(filepath.Base(rel))
		dirPart := strings.ToLower(strings.TrimSuffix(rel, filepath.Base(rel)))
		for _, t := range terms {
			if strings.Contains(base, t) {
				score += 5 // basename hit: strong
			} else if strings.Contains(dirPart, t) {
				score += 2 // dir/path hit
			}
		}

		var snippet, sym string
		var defLine int
		info, _ := d.Info()
		if info != nil && info.Size() <= 1<<20 {
			if data, e := os.ReadFile(p); e == nil && !isBinary(data) {
				cs, ln, s, snip := scoreContent(string(data), terms)
				score += cs
				defLine, sym, snippet = ln, s, snip
			}
		}
		if score > 0 {
			ranked = append(ranked, rankedFile{Path: rel, Score: score, Line: defLine, Symbol: sym, Snippet: snippet})
		}
		return nil
	})

	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		return ranked[i].Path < ranked[j].Path
	})
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	return okJSON("", ranked), nil
}

// scoreContent scores a file body against the terms and returns the best symbol
// definition (1-based line, name, snippet) it found. Ranking, strongest first:
//   - a query term that EXACTLY names a defined symbol (`func ParseConfig` for
//     "parseconfig"): the surest "edit here" signal (+16, or +12 on a camelCase
//     subtoken like "parse"); this points at the definition site, not a caller.
//   - a definition line otherwise mentioning a term in its head (+6).
//   - plain content mentions (+1 per distinct term) + a multi-term coverage bonus.
func scoreContent(content string, terms []string) (score, defLine int, defSym, snippet string) {
	lower := strings.ToLower(content)
	present := 0
	for _, t := range terms {
		if strings.Contains(lower, t) {
			present++
			score++ // distinct-term mention
		}
	}
	if present == 0 {
		return 0, 0, "", ""
	}
	if present >= 2 {
		score += 2 * (present - 1) // coverage bonus: files hitting more terms rank up
	}

	var anySnippet string
	bestDef := -1 // score of the best symbol-definition hit so far
	for i, line := range strings.Split(content, "\n") {
		ll := strings.ToLower(line)
		// Best snippet fallback: first line mentioning any term.
		if anySnippet == "" {
			for _, t := range terms {
				if strings.Contains(ll, t) {
					anySnippet = oneLineN(strings.TrimSpace(line), 120)
					break
				}
			}
		}
		// Only structural definition lines carry the "edit here" weight — this
		// gate keeps comments like "// call func handler()" from scoring as defs.
		if !defLineRE.MatchString(line) {
			continue
		}
		// Symbol-name matching: extract the declared identifier(s) and compare to
		// the query terms by match strength (exact identifier > camel subtoken >
		// substring), so the definition site outranks callers.
		matched := false
		for _, name := range defNames(line) {
			ln := strings.ToLower(name)
			sub := map[string]bool{ln: true}
			for _, s := range splitCamel(name) {
				sub[strings.ToLower(s)] = true
			}
			for _, p := range strings.Split(ln, "_") {
				sub[p] = true
			}
			hit := 0
			for _, t := range terms {
				switch {
				case ln == t:
					hit += 16
				case sub[t]:
					hit += 12
				case strings.Contains(ln, t):
					hit += 6
				}
			}
			if hit > 0 {
				matched = true
				score += hit
				if hit > bestDef {
					bestDef = hit
					defLine = i + 1
					defSym = name
					snippet = oneLineN(strings.TrimSpace(line), 120)
				}
			}
		}
		// A def line that mentions a term in its head but whose name didn't match
		// (e.g. a method on a queried type) is still a weak locate signal.
		if !matched && bestDef < 0 {
			head := ll
			if j := strings.IndexAny(ll, "({=:"); j >= 0 {
				head = ll[:j]
			}
			for _, t := range terms {
				if strings.Contains(head, t) {
					score += 6
					if snippet == "" {
						defLine = i + 1
						snippet = oneLineN(strings.TrimSpace(line), 120)
					}
					break
				}
			}
		}
	}
	if snippet == "" {
		snippet = anySnippet
	}
	return score, defLine, defSym, snippet
}

// stopwords are generic words dropped from queries to cut ranking noise.
var stopwords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "that": true, "this": true,
	"from": true, "into": true, "not": true, "are": true, "was": true, "use": true,
	"when": true, "where": true, "which": true, "what": true, "how": true, "why": true,
	"add": true, "make": true, "get": true, "set": true, "fix": true, "bug": true,
	"issue": true, "should": true, "would": true, "could": true, "does": true, "doesnt": true,
	"file": true, "files": true, "code": true, "line": true, "value": true, "values": true,
}

// keywords tokenizes a query into distinct lowercase terms (>=3 bytes), splitting
// snake_case and camelCase into subtokens and dropping generic stopwords, so
// "parseConfig" and "parse_config" both yield {parse, config}. Word chars are
// Unicode letters/digits, so a Korean/CJK/Cyrillic query tokenizes too (a single
// CJK syllable is 3 bytes, so the length gate admits it — those are morpheme-dense).
func keywords(q string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(w string) {
		w = strings.ToLower(w)
		if len(w) >= 3 && !seen[w] && !stopwords[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	// Split on non-word runes (handles snake_case, spaces, punctuation). Word runes
	// are Unicode letters/digits — an ASCII-only predicate here dropped every rune of
	// a non-Latin query, dead-ending findcontext with "no usable keywords" on Korean/
	// CJK/Cyrillic codebases and prompts.
	for _, tok := range strings.FieldsFunc(q, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r))
	}) {
		add(tok) // the whole token (e.g. parseconfig)
		for _, sub := range splitCamel(tok) {
			add(sub) // camelCase subtokens (parse, config)
		}
	}
	return out
}

// splitCamel breaks camelCase / PascalCase / digit boundaries into parts.
func splitCamel(s string) []string {
	var parts []string
	start := 0
	for i := 1; i < len(s); i++ {
		prev, cur := rune(s[i-1]), rune(s[i])
		boundary := (prev >= 'a' && prev <= 'z' && cur >= 'A' && cur <= 'Z') ||
			((prev < '0' || prev > '9') && cur >= '0' && cur <= '9')
		if boundary {
			parts = append(parts, s[start:i])
			start = i
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func oneLineN(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
