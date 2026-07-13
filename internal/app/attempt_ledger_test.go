package app

import (
	"encoding/json"
	"strings"
	"testing"
)

// attemptSig coarsely normalizes an action so near-duplicate attempts collide on one ledger
// entry: a bash line keeps its verb plus the first non-flag token (flags/paths/redirects that
// vary run-to-run drop out), and a distinct subcommand stays distinct.
func TestAttemptSigCoarseCollision(t *testing.T) {
	// Same grep target, different flags/file ⇒ one signature.
	if a, b := attemptSig("bash", "grep foo -n a.go"), attemptSig("bash", "grep -rn foo b.go"); a != b {
		t.Errorf("near-duplicate greps must share a signature: %q vs %q", a, b)
	}
	// Different subcommand ⇒ distinct signature (don't fold unrelated work).
	if a, b := attemptSig("bash", "go test ./x"), attemptSig("bash", "go build ./x"); a == b {
		t.Errorf("distinct subcommands must not collide: both %q", a)
	}
	// A search tool folds to name+query, case/space-insensitive.
	if a, b := attemptSig("grep", "  Foo   Bar "), attemptSig("grep", "foo bar"); a != b {
		t.Errorf("search-tool signature must normalize case/space: %q vs %q", a, b)
	}
	// A bare command with only flags still yields a stable non-empty signature.
	if s := attemptSig("bash", "ls -la"); s == "" {
		t.Errorf("signature must not be empty for a flag-only command")
	}
}

// noteAttempt is idempotent per signature: a recurrence bumps the count instead of adding a
// second line, the outcome is single-lined and clipped, and a first sighting with no reason is
// backfilled by a later one.
func TestNoteAttemptIdempotentAndClips(t *testing.T) {
	g := newRunGuard()
	g.noteAttempt("empty", "grep foo", "grep foo", "")           // first sighting, no reason
	g.noteAttempt("empty", "grep foo", "grep foo", "no matches") // recurrence backfills reason
	g.noteAttempt("empty", "grep foo", "grep foo", "no matches") // recurrence bumps count

	if len(g.attemptOrder) != 1 {
		t.Fatalf("same signature must not add multiple order entries; got %d", len(g.attemptOrder))
	}
	rec := g.attempts["grep foo"]
	if rec.count != 3 {
		t.Errorf("count must track recurrences; got %d", rec.count)
	}
	if rec.outcome != "no matches" {
		t.Errorf("a later reason must backfill an empty first outcome; got %q", rec.outcome)
	}

	// A blank signature is ignored entirely.
	g.noteAttempt("fail", "  ", "whatever", "boom")
	if len(g.attemptOrder) != 1 {
		t.Errorf("a blank signature must be ignored; order grew to %d", len(g.attemptOrder))
	}

	// Outcome is single-lined and clipped to the snippet cap.
	long := strings.Repeat("x", tabuSnippetCap+50)
	g.noteAttempt("fail", "go test ./p", "go test ./p", "line one\nline two "+long)
	got := g.attempts["go test ./p"].outcome
	if strings.Contains(got, "\n") {
		t.Errorf("outcome must be single-lined; got %q", got)
	}
	if len(got) > tabuSnippetCap+len("…") {
		t.Errorf("outcome must be clipped to the snippet cap; got len %d", len(got))
	}
}

// triedDigest renders newest-last, bounds to maxN, and is empty on a clean run so injection
// sites add nothing.
func TestTriedDigestBoundsAndFormat(t *testing.T) {
	g := newRunGuard()
	if d := g.triedDigest(8); d != "" {
		t.Errorf("empty ledger must yield an empty digest; got %q", d)
	}
	for _, n := range []string{"a", "b", "c", "d"} {
		g.noteAttempt("empty", "grep "+n, "grep "+n, "no matches")
	}
	g.noteAttempt("empty", "grep d", "grep d", "no matches") // recurrence → ×2, no new line

	dig := g.triedDigest(2)
	lines := strings.Split(dig, "\n")
	if len(lines) != 2 {
		t.Fatalf("digest must bound to maxN lines; got %d:\n%s", len(lines), dig)
	}
	// Newest-last: the last two distinct signatures are grep c then grep d.
	if !strings.Contains(lines[0], "grep c") || !strings.Contains(lines[1], "grep d") {
		t.Errorf("digest must render newest-last; got:\n%s", dig)
	}
	if !strings.Contains(lines[1], "(×2)") {
		t.Errorf("a repeated dead-end must show its count; got %q", lines[1])
	}
	if !strings.HasPrefix(lines[0], "- empty ") || !strings.Contains(lines[0], "→ no matches") {
		t.Errorf("digest line format is wrong; got %q", lines[0])
	}
	if d := g.triedDigest(0); d != "" {
		t.Errorf("maxN<=0 must yield an empty digest; got %q", d)
	}
}

// attemptTarget pulls a display target from a tool call: the bash command, or the first
// meaningful string arg of a structured tool; "" when nothing usable is present.
func TestAttemptTarget(t *testing.T) {
	if got := attemptTarget("bash", json.RawMessage(`{"command":" grep foo "}`)); got != "grep foo" {
		t.Errorf("bash target must be the trimmed command; got %q", got)
	}
	if got := attemptTarget("grep", json.RawMessage(`{"pattern":"needle","path":"x"}`)); got != "needle" {
		t.Errorf("structured target must prefer pattern; got %q", got)
	}
	if got := attemptTarget("list", json.RawMessage(`{"dir":"pkg"}`)); got != "pkg" {
		t.Errorf("structured target must fall through to a later key; got %q", got)
	}
	if got := attemptTarget("grep", json.RawMessage(`{"limit":5}`)); got != "" {
		t.Errorf("no usable string arg must yield an empty target; got %q", got)
	}
}

// isNoMatchResult flags an empty body or a common no-match marker, and leaves real content
// alone (a false "empty" would be noise in an advisory ledger).
func TestIsNoMatchResult(t *testing.T) {
	for _, empty := range []string{"", "   ", "No matches found", "0 matches", "not found", "no results"} {
		if !isNoMatchResult(empty) {
			t.Errorf("expected no-match for %q", empty)
		}
	}
	for _, hit := range []string{"foo.go:12: match", "found 3 files"} {
		if isNoMatchResult(hit) {
			t.Errorf("real content must not read as no-match: %q", hit)
		}
	}
}

// normFocus lowercases and whitespace-collapses so near-identical fan-out targets fold to one.
func TestNormFocus(t *testing.T) {
	if a, b := normFocus(" The  Parser "), normFocus("the parser"); a != b {
		t.Errorf("normFocus must fold case/whitespace: %q vs %q", a, b)
	}
	if normFocus("   ") != "" {
		t.Errorf("blank focus must normalize to empty")
	}
}

// explorerPrompt appends the parent's dead-ends only when non-empty, so a first plan's brief
// is unchanged and a re-plan's brief carries the burned approaches.
func TestExplorerPromptInjectsTried(t *testing.T) {
	g := planGroup{Agent: "explore", Focus: "the parser", Question: "how are tokens emitted?"}
	clean := explorerPrompt("goal", g, "")
	if strings.Contains(clean, "Already attempted") {
		t.Errorf("no dead-end block should appear for an empty ledger; got:\n%s", clean)
	}
	primed := explorerPrompt("goal", g, "- empty grep foo → no matches")
	if !strings.Contains(primed, "Already attempted") || !strings.Contains(primed, "grep foo") {
		t.Errorf("dead-end block must be injected when the ledger is non-empty; got:\n%s", primed)
	}
}

// capGroups drops near-duplicate focuses before trimming to the per-turn budget, so a repeat
// can't crowd a distinct target out of the budget.
func TestCapGroupsDedupsBeforeTrim(t *testing.T) {
	budget := 2
	groups := []planGroup{
		{Focus: "the parser"},
		{Focus: "The  Parser"}, // near-duplicate of the first
		{Focus: "the lexer"},
	}
	got := capGroups(groups, &budget)
	if len(got) != 2 {
		t.Fatalf("expected 2 distinct groups after dedup; got %d", len(got))
	}
	if normFocus(got[0].Focus) == normFocus(got[1].Focus) {
		t.Errorf("deduped groups must be distinct; got %q and %q", got[0].Focus, got[1].Focus)
	}
	// The distinct lexer survived rather than being crowded out by the parser duplicate.
	if got[1].Focus != "the lexer" {
		t.Errorf("distinct target must survive the dedup+trim; got %q", got[1].Focus)
	}
	if budget != 0 {
		t.Errorf("budget must be decremented by kept groups; got %d", budget)
	}
}

// parseList folds near-duplicate work-items so the scout doesn't fan two explorers at the same
// target.
func TestParseListDedups(t *testing.T) {
	got := parseList("config.go\nConfig.go\nmain.go\nconfig.go")
	if len(got) != 2 {
		t.Fatalf("expected 2 distinct items after dedup; got %d: %v", len(got), got)
	}
}
