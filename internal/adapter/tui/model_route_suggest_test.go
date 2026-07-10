package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sayaya1090/magi/internal/app"
)

// enterSessionEdit opens the editor and begins editing the (session) row with the
// given catalog loaded, so the suggest box is populated.
func enterSessionEdit(t *testing.T, m *Model, catalog []string) {
	t.Helper()
	m.openRouteEditor()
	m.modelCatalog = catalog
	m.catalogLoaded = true
	if m.routeList[m.routeSel].kind != rowSession {
		t.Fatalf("row 0 should be the session row, got %v", m.routeList[m.routeSel].kind)
	}
	m.handleRouteKey(keyPress("enter")) // begin editing the session row
	if !m.routeEditing {
		t.Fatal("enter should begin editing the session row")
	}
}

// The suggest list merges configured profile models FIRST, then the gateway
// catalog, de-duplicating shared IDs (each appears once).
func TestModelSuggestionsMergeDedup(t *testing.T) {
	m := routedTestModel(t)
	m.app.SetProfile(app.ProfileDef{Name: "z1", Model: "p1"})
	m.app.SetProfile(app.ProfileDef{Name: "z2", Model: "shared"})
	m.modelCatalog = []string{"shared", "a", "a"} // dup within catalog + dup vs profile
	got := m.modelSuggestions()
	want := []string{"p1", "shared", "a"}
	if len(got) != len(want) {
		t.Fatalf("suggestions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("suggestions = %v, want %v (profiles first, deduped)", got, want)
		}
	}
}

// An empty routeBuf shows the merged head; typing filters case-insensitively by
// substring.
func TestModelSuggestionsFilter(t *testing.T) {
	m := routedTestModel(t)
	m.modelCatalog = []string{"gpt-oss:120b", "qwen3-coder", "GPT-mini"}
	m.routeBuf = ""
	if len(m.modelSuggestions()) != 3 {
		t.Fatalf("empty buffer should show all, got %v", m.modelSuggestions())
	}
	m.routeBuf = "gpt"
	got := m.modelSuggestions()
	if len(got) != 2 || got[0] != "gpt-oss:120b" || got[1] != "GPT-mini" {
		t.Fatalf("case-insensitive substring filter = %v, want gpt-oss + GPT-mini", got)
	}
}

// The list is capped at modelSugMax so a huge catalog can't blow up the box.
func TestModelSuggestionsCap(t *testing.T) {
	m := routedTestModel(t)
	big := make([]string, 50)
	for i := range big {
		big[i] = string(rune('a'+i%26)) + "-model"
	}
	m.modelCatalog = big
	if n := len(m.modelSuggestions()); n != modelSugMax {
		t.Fatalf("suggestions len = %d, want cap %d", n, modelSugMax)
	}
}

// With no gateway catalog the box is still useful: configured profile models fill it.
func TestModelSuggestionsEmptyCatalogUsesProfiles(t *testing.T) {
	m := routedTestModel(t)
	m.app.SetProfile(app.ProfileDef{Name: "fast", Model: "gpt-oss:20b"})
	m.modelCatalog = nil
	got := m.modelSuggestions()
	if len(got) != 1 || got[0] != "gpt-oss:20b" {
		t.Fatalf("profiles should populate the box without a catalog, got %v", got)
	}
}

// ↑/↓ move through the suggestions circularly; entering navigation from free text
// (-1) lands on an end, and a full cycle of downs returns to the start.
func TestRouteSuggestCircularNav(t *testing.T) {
	m := routedTestModel(t)
	enterSessionEdit(t, &m, []string{"m0", "m1", "m2"})
	n := 3

	// From free text, up lands on the last entry, down on the first.
	if m.modelSugSel != -1 {
		t.Fatalf("edit should start in free text (-1), got %d", m.modelSugSel)
	}
	m.handleRouteKey(keyPress("up"))
	if m.modelSugSel != n-1 {
		t.Fatalf("up from free text: sel = %d, want %d", m.modelSugSel, n-1)
	}
	m.modelSugSel = -1
	m.handleRouteKey(keyPress("down"))
	if m.modelSugSel != 0 {
		t.Fatalf("down from free text: sel = %d, want 0", m.modelSugSel)
	}
	// Wrap at the ends.
	m.modelSugSel = 0
	m.handleRouteKey(keyPress("up"))
	if m.modelSugSel != n-1 {
		t.Fatalf("up from top: sel = %d, want %d (wrap)", m.modelSugSel, n-1)
	}
	m.modelSugSel = n - 1
	m.handleRouteKey(keyPress("down"))
	if m.modelSugSel != 0 {
		t.Fatalf("down from bottom: sel = %d, want 0 (wrap)", m.modelSugSel)
	}
	// A full cycle of downs is identity.
	m.modelSugSel = 0
	for i := 0; i < n; i++ {
		m.handleRouteKey(keyPress("down"))
	}
	if m.modelSugSel != 0 {
		t.Fatalf("after %d downs sel = %d, want 0", n, m.modelSugSel)
	}
}

// Tab accepts the highlighted suggestion into the buffer (then back to free text).
func TestRouteSuggestTabFills(t *testing.T) {
	m := routedTestModel(t)
	enterSessionEdit(t, &m, []string{"m0", "m1", "m2"})
	m.modelSugSel = 1
	m.handleRouteKey(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.routeBuf != "m1" {
		t.Fatalf("tab should fill buffer with m1, got %q", m.routeBuf)
	}
	if m.modelSugSel != -1 {
		t.Fatalf("tab should return to free text, sel = %d", m.modelSugSel)
	}
	// With nothing highlighted, tab fills the first suggestion.
	m.routeBuf = ""
	m.modelSugSel = -1
	m.handleRouteKey(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.routeBuf != "m0" {
		t.Fatalf("tab with no highlight should fill first, got %q", m.routeBuf)
	}
}

// enter applies the highlighted suggestion (not the typed buffer) on the session row.
func TestRouteSuggestEnterAppliesSuggestion(t *testing.T) {
	m := routedTestModel(t)
	enterSessionEdit(t, &m, []string{"m0", "m1", "m2"})
	m.modelSugSel = 2
	m.handleRouteKey(keyPress("enter"))
	if m.model != "m2" {
		t.Fatalf("enter should apply the highlighted suggestion, model = %q", m.model)
	}
	if m.routeEditing {
		t.Error("enter should exit editing")
	}
	if m.app.SessionModel(m.sid) != "m2" {
		t.Errorf("session model not set to m2: %q", m.app.SessionModel(m.sid))
	}
}

// With no suggestion highlighted, enter applies the free-text buffer verbatim —
// so a model not in the catalog is still settable.
func TestRouteSuggestEnterFreeTextFallback(t *testing.T) {
	m := routedTestModel(t)
	enterSessionEdit(t, &m, []string{"m0", "m1"})
	typeStr(&m, "custom-model")
	if m.modelSugSel != -1 {
		t.Fatalf("typing should keep free text, sel = %d", m.modelSugSel)
	}
	m.handleRouteKey(keyPress("enter"))
	if m.model != "custom-model" {
		t.Fatalf("free-text enter should apply the buffer, model = %q", m.model)
	}
}

// Typing after selecting a suggestion drops the highlight and re-filters.
func TestRouteSuggestTypingResetsSelection(t *testing.T) {
	m := routedTestModel(t)
	enterSessionEdit(t, &m, []string{"m0", "m1", "m2"})
	m.modelSugSel = 2
	m.handleRouteKey(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if m.modelSugSel != -1 {
		t.Fatalf("typing should reset selection to free text, sel = %d", m.modelSugSel)
	}
	if m.routeBuf != "x" {
		t.Fatalf("typed char should append to buffer, got %q", m.routeBuf)
	}
}

// When there are no suggestions (no catalog, no profiles) ↑/↓ are inert and enter
// still applies the typed buffer — the pre-existing free-text UX is preserved.
func TestRouteSuggestFallbackNoCatalog(t *testing.T) {
	m := routedTestModel(t)
	m.openRouteEditor()
	m.modelCatalog = nil
	m.catalogLoaded = true
	m.handleRouteKey(keyPress("enter")) // edit session row
	if len(m.modelSuggestions()) != 0 {
		t.Fatalf("precondition: no suggestions, got %v", m.modelSuggestions())
	}
	m.handleRouteKey(keyPress("up"))
	m.handleRouteKey(keyPress("down"))
	if m.modelSugSel != -1 {
		t.Fatalf("arrows must be inert without suggestions, sel = %d", m.modelSugSel)
	}
	typeStr(&m, "free-typed")
	m.handleRouteKey(keyPress("enter"))
	if m.model != "free-typed" {
		t.Fatalf("free-text enter should still work, model = %q", m.model)
	}
}

// The suggest box must not hijack agent rows: while editing an agent row ↑/↓ stay
// inert (no modelSugSel movement) and ←/→ still cycles profiles, even with a
// catalog loaded.
func TestRouteSuggestLeavesAgentRowUnchanged(t *testing.T) {
	m := routedTestModel(t)
	m.app.SetProfile(app.ProfileDef{Name: "fast", Model: "gpt-oss:20b"})
	m.openRouteEditor()
	m.modelCatalog = []string{"m0", "m1", "m2"} // a catalog exists, but this is an agent row
	m.catalogLoaded = true
	m.handleRouteKey(keyPress("down")) // → an agent row (row 1)
	if m.routeList[m.routeSel].kind != rowAgent {
		t.Fatalf("expected an agent row, got %v", m.routeList[m.routeSel].kind)
	}
	m.handleRouteKey(keyPress("enter")) // begin editing the agent row
	m.handleRouteKey(keyPress("up"))
	m.handleRouteKey(keyPress("down"))
	if m.modelSugSel != -1 {
		t.Fatalf("arrows must not drive the suggest box on an agent row, sel = %d", m.modelSugSel)
	}
	m.handleRouteKey(keyPress("right")) // profile picker still works
	if m.routeBuf != "fast" {
		t.Fatalf("→ should still cycle profiles on an agent row, buf = %q", m.routeBuf)
	}
}

// Before the catalog loads, the box shows a single "loading…" line and the editor
// height reserves exactly that one extra row.
func TestModelSuggestBoxLoading(t *testing.T) {
	m := routedTestModel(t)
	m.openRouteEditor()
	m.catalogLoaded = false // still fetching, no profiles defined
	m.handleRouteKey(keyPress("enter"))
	box := m.modelSuggestBox()
	if !strings.Contains(box, "loading") {
		t.Fatalf("pre-load box should show a loading hint, got %q", box)
	}
	if got := strings.Count(box, "\n"); got != 1 {
		t.Fatalf("loading box should be one line, got %d newlines in %q", got, box)
	}
}

// A stale/out-of-range modelSugSel collapses to free text (-1) before use.
func TestClampModelSug(t *testing.T) {
	m := routedTestModel(t)
	cases := []struct {
		sel, n, want int
	}{
		{sel: 100, n: 3, want: -1}, // past the end
		{sel: -5, n: 3, want: -1},  // below -1
		{sel: 2, n: 3, want: 2},    // in range, kept
		{sel: -1, n: 3, want: -1},  // free text, kept
		{sel: 1, n: 0, want: -1},   // empty list collapses
	}
	for _, c := range cases {
		m.modelSugSel = c.sel
		m.clampModelSug(c.n)
		if m.modelSugSel != c.want {
			t.Errorf("clampModelSug(sel=%d,n=%d) = %d, want %d", c.sel, c.n, m.modelSugSel, c.want)
		}
	}
}

// modelCatalogMsg caches the catalog and marks it loaded so the box stops showing
// "loading…" and re-opens don't re-fetch.
func TestModelCatalogMsgUpdatesState(t *testing.T) {
	m := routedTestModel(t)
	updated, _ := m.Update(modelCatalogMsg{models: []string{"x", "y"}})
	mm := updated.(Model)
	if !mm.catalogLoaded {
		t.Fatal("catalogLoaded should be set after the message")
	}
	if len(mm.modelCatalog) != 2 || mm.modelCatalog[0] != "x" {
		t.Fatalf("catalog not stored: %v", mm.modelCatalog)
	}
}

// The catalog is prefetched exactly once per session: the first open returns a
// fetch command, later opens (catalog already loaded) return nil.
func TestOpenRouteEditorPrefetchesOnce(t *testing.T) {
	m := routedTestModel(t)
	if cmd := m.openRouteEditor(); cmd == nil {
		t.Fatal("first open should return a prefetch command")
	}
	m.catalogLoaded = true
	if cmd := m.openRouteEditor(); cmd != nil {
		t.Error("second open with a loaded catalog should not re-fetch")
	}
}
