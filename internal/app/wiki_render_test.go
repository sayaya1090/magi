package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// splitWiki answers the index half and the search half separately, which the advertisement's own
// fake cannot: wikiPointer joins two stores' answers and the interesting cases are the ones where
// they disagree (an index with no live hit, a hit with no index, either half erroring alone).
type splitWiki struct {
	port.ExperienceStore
	index, hits         []port.WikiPage
	indexErr, searchErr error
}

func (s *splitWiki) WikiIndex(ctx context.Context, n int) ([]port.WikiPage, error) {
	return s.index, s.indexErr
}
func (s *splitWiki) WikiSearch(ctx context.Context, q string, n int) ([]port.WikiPage, error) {
	return s.hits, s.searchErr
}

// The pointer's job is to advertise a LIVE page. An exact-title match on a retired page leads the
// ranking by design — a tombstone is still the best answer to its own title — so the leader being
// stale must not cost the fresh runner-up its slot.
func TestAStalePageDoesNotSilenceTheFreshOneBehindIt(t *testing.T) {
	// Two live pages behind the tombstone, not one: with a single fresh hit the block is one line
	// whether or not the loop stops after it, and "exactly one pointer" would then be a fact about
	// the fixture instead of about the code.
	w := &splitWiki{hits: []port.WikiPage{
		{Title: "auth flow", Body: "this stopped being true", Stale: true},
		{Title: "auth flow v2", Body: "the token rides the header"},
		{Title: "auth flow notes", Body: "third in the ranking"},
	}}

	got := wikiPointer(context.Background(), w, "auth flow")

	if strings.Contains(got, "[auth flow]") {
		t.Errorf("the retired page must not be advertised: %q", got)
	}
	if !strings.Contains(got, "[auth flow v2]") || !strings.Contains(got, "the token rides the header") {
		t.Errorf("the fresh runner-up must take the slot: %q", got)
	}
	if n := strings.Count(got, "wiki page likely relevant"); n != 1 {
		t.Errorf("the pointer names ONE page — the rest is what recall_memory is for; got %d: %q", n, got)
	}
	if strings.Contains(got, "third in the ranking") {
		t.Errorf("the ranking below the winner does not ride the prompt: %q", got)
	}
}

// All stale is not "pick the least stale" — it is nothing to advertise.
func TestAllStaleHitsAdvertiseNoPage(t *testing.T) {
	w := &splitWiki{hits: []port.WikiPage{
		{Title: "a", Body: "gone", Stale: true},
		{Title: "b", Body: "also gone", Stale: true},
	}}

	if got := wikiPointer(context.Background(), w, "a"); got != "" {
		t.Errorf("no live page means no pointer, got %q", got)
	}
}

// The separator is conditional on the index half having written something. Unconditional "\n"
// would open the block with a blank line, which reads as a dropped section.
func TestThePointerDoesNotOpenWithABlankLineWhenTheIndexIsEmpty(t *testing.T) {
	w := &splitWiki{hits: []port.WikiPage{{Title: "auth", Body: "live"}}}

	got := wikiPointer(context.Background(), w, "auth")

	if strings.HasPrefix(got, "\n") {
		t.Errorf("no index half means no leading newline, got %q", got)
	}
	if !strings.HasPrefix(got, "wiki page likely relevant") {
		t.Errorf("the pointer is the whole block here, got %q", got)
	}
}

// Both halves render: the index says the pages exist, the pointer says which one answers this.
func TestBothHalvesRenderOnTheirOwnLines(t *testing.T) {
	w := &splitWiki{
		index: []port.WikiPage{{Title: "auth flow"}, {Title: "deploy"}},
		hits:  []port.WikiPage{{Title: "deploy", Body: "podman, not docker"}},
	}

	lines := strings.Split(wikiPointer(context.Background(), w, "deploy"), "\n")

	if len(lines) != 2 {
		t.Fatalf("index line then pointer line, got %d: %q", len(lines), lines)
	}
	if !strings.Contains(lines[0], "auth flow · deploy") {
		t.Errorf("the index lists titles separated by ·, got %q", lines[0])
	}
	if !strings.Contains(lines[1], "[deploy] podman, not docker") {
		t.Errorf("the pointer carries the title and the hook, got %q", lines[1])
	}
}

// A store that cannot answer is silent, not an error in the prompt — and one half failing does not
// take the other down with it.
func TestAFailingHalfGoesSilentAndTheOtherStillSpeaks(t *testing.T) {
	boom := errors.New("store unreachable")
	ctx := context.Background()

	// Each failing half hands back data AS WELL AS the error. A nil slice would make these pass
	// on emptiness rather than on the guard: `err == nil && len(idx) > 0` and its search twin are
	// what is under test, and neither is reached by a store that simply answered nothing.
	onlyPointer := wikiPointer(ctx, &splitWiki{
		index:    []port.WikiPage{{Title: "half-read index"}},
		indexErr: boom,
		hits:     []port.WikiPage{{Title: "deploy", Body: "live"}},
	}, "deploy")
	if strings.Contains(onlyPointer, "wiki pages (") || !strings.Contains(onlyPointer, "[deploy]") {
		t.Errorf("a failed index must not stop the pointer: %q", onlyPointer)
	}

	if strings.Contains(onlyPointer, "half-read index") {
		t.Errorf("what a failed index handed back is not advertised: %q", onlyPointer)
	}

	onlyIndex := wikiPointer(ctx, &splitWiki{
		index:     []port.WikiPage{{Title: "deploy"}},
		hits:      []port.WikiPage{{Title: "half-read hit", Body: "live"}},
		searchErr: boom,
	}, "deploy")
	if !strings.Contains(onlyIndex, "wiki pages (") || strings.Contains(onlyIndex, "likely relevant") {
		t.Errorf("a failed search must not stop the index: %q", onlyIndex)
	}
	if strings.Contains(onlyIndex, "half-read hit") {
		t.Errorf("what a failed search handed back is not advertised: %q", onlyIndex)
	}

	both := wikiPointer(ctx, &splitWiki{
		index:     []port.WikiPage{{Title: "deploy"}},
		hits:      []port.WikiPage{{Title: "deploy", Body: "live"}},
		indexErr:  boom,
		searchErr: boom,
	}, "deploy")
	if both != "" {
		t.Errorf("both halves down is an empty block, got %q", both)
	}
}

// Titles and hooks are model-authored and fleet-synced, and this block rides EVERY step's prompt.
// The bound is the whole reason the advertisement is affordable.
func TestTitlesAndHooksAreBoundedBeforeTheyRideEveryStep(t *testing.T) {
	long := strings.Repeat("t", 500)
	w := &splitWiki{
		index: []port.WikiPage{{Title: long}},
		hits:  []port.WikiPage{{Title: long, Body: strings.Repeat("b", 500)}},
	}

	got := wikiPointer(context.Background(), w, "t")

	if strings.Contains(got, strings.Repeat("t", 81)) {
		t.Errorf("a title rides clipped to 80, got %q", got)
	}
	if strings.Contains(got, strings.Repeat("b", 161)) {
		t.Errorf("a hook rides clipped to 160, got %q", got)
	}
}

// The hook is the first line a reader would see, not the first bytes: a page that opens with blank
// lines would otherwise advertise itself as "".
func TestTheHookSkipsTheBlankOpeningOfAPage(t *testing.T) {
	if got := firstNonBlankLine("\n\n   \n  the answer  \nmore\n"); got != "the answer" {
		t.Errorf("first non-blank line, trimmed: got %q", got)
	}
	if got := firstNonBlankLine("   \n\t\n"); got != "" {
		t.Errorf("a page with nothing to say says nothing, got %q", got)
	}
	if got := firstNonBlankLine(""); got != "" {
		t.Errorf("empty in, empty out, got %q", got)
	}
}

// A recall hands the model a CANONICAL claim, so it also hands over the material to judge it:
// which tier it won in, who last edited it and when — and whether it has been retired, because
// "this stopped being true" is an answer, not an omission.
func TestARecalledPageCarriesWhatItTakesToTrustIt(t *testing.T) {
	got := formatWikiPages([]port.WikiPage{{
		Title:   "auth flow",
		Body:    "the token rides the header",
		Tier:    "team",
		Editor:  "magi-9f",
		Updated: "2026-08-29T09:00:00Z",
		Stale:   true,
		Links:   []string{"deploy", "tokens"},
	}})

	for _, want := range []string{
		"- wiki:team",
		"[auth flow]",
		"⚠STALE",
		"(magi-9f, 2026-08-29T09:00:00Z)",
		"  the token rides the header",
		"  related: deploy, tokens",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("a recalled page must carry %q:\n%s", want, got)
		}
	}
}

// Every head field is optional and each absence is silent — no empty tier suffix, no bare "()",
// no stale mark on a live page, no "related:" line for a page that links nowhere.
func TestAPageWithNothingToDeclareDeclaresNothing(t *testing.T) {
	got := formatWikiPages([]port.WikiPage{{Title: "bare", Body: "just a body"}})

	if got != "- wiki [bare]\n  just a body" {
		t.Errorf("a bare page renders bare, got %q", got)
	}
}

// Half a head is still a head: an editor with no timestamp must not drag in the separator that
// joins the two, and a timestamp with no editor must not open with one.
func TestTheHeadSeparatorOnlyAppearsBetweenTwoThings(t *testing.T) {
	editorOnly := formatWikiPages([]port.WikiPage{{Title: "a", Editor: "magi-a3"}})
	if !strings.Contains(editorOnly, "(magi-a3)") || strings.Contains(editorOnly, ", )") {
		t.Errorf("an editor alone stands alone, got %q", editorOnly)
	}
	stampOnly := formatWikiPages([]port.WikiPage{{Title: "a", Updated: "2026-08-29"}})
	if !strings.Contains(stampOnly, "(2026-08-29)") || strings.Contains(stampOnly, "(, ") {
		t.Errorf("a timestamp alone stands alone, got %q", stampOnly)
	}
}

// A body is quoted at one indent so the head stays the only thing at the left margin — every line
// of it, not just the first, or a multi-line page would break out of its own entry.
func TestEveryLineOfABodyIsIndentedUnderItsHead(t *testing.T) {
	got := formatWikiPages([]port.WikiPage{{Title: "a", Body: "first\nsecond\nthird"}})

	for _, line := range strings.Split(got, "\n")[1:] {
		if !strings.HasPrefix(line, "  ") {
			t.Errorf("a body line escaped the indent: %q\n%s", line, got)
		}
	}
}

// Bounded for the same reason the advertisement is: a page body and a SYNCED revision's
// frontmatter are model-authored text with no ceiling at write time, and one bloated page must
// not own the context a recall was meant to inform. The marker says where the rest is.
func TestABloatedPageIsCutAndSaysSo(t *testing.T) {
	got := formatWikiPages([]port.WikiPage{{
		Title:   strings.Repeat("t", 300),
		Body:    strings.Repeat("b", 4000),
		Editor:  strings.Repeat("e", 300),
		Updated: strings.Repeat("u", 300),
		Links:   []string{strings.Repeat("l", 400)},
	}})

	for _, tc := range []struct {
		what   string
		tooBig string
	}{
		{"title", strings.Repeat("t", 81)},
		{"body", strings.Repeat("b", 2401)},
		{"editor", strings.Repeat("e", 61)},
		{"timestamp", strings.Repeat("u", 41)},
		{"links", strings.Repeat("l", 201)},
	} {
		if strings.Contains(got, tc.tooBig) {
			t.Errorf("the %s rode into context unclipped", tc.what)
		}
	}
	if !strings.Contains(got, "the rest of this page is not shown") {
		t.Errorf("a cut body must say it was cut:\n%s", got)
	}
}

// A page whose body is empty (or only whitespace) contributes a head and no quoted block —
// an indented blank line reads as a page that said something unreadable.
func TestAnEmptyBodyLeavesNoEmptyQuote(t *testing.T) {
	got := formatWikiPages([]port.WikiPage{{Title: "a", Body: "   \n\t\n"}})

	if got != "- wiki [a]" {
		t.Errorf("no body means no quoted block, got %q", got)
	}
}

// Several pages come back from one recall, and each is a whole entry — the trailing newline is
// trimmed once at the end, not between them.
func TestPagesStackWithoutRunningTogether(t *testing.T) {
	got := formatWikiPages([]port.WikiPage{
		{Title: "a", Body: "one"},
		{Title: "b", Body: "two"},
	})

	if got != "- wiki [a]\n  one\n- wiki [b]\n  two" {
		t.Errorf("two entries, no blank line between and none trailing, got %q", got)
	}
	if formatWikiPages(nil) != "" {
		t.Errorf("nothing recalled renders nothing")
	}
}
