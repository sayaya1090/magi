package tui

import (
	"strings"
	"testing"
)

// Forensic probes for the emoji-narrow width correction (width.go/panel.go).
// Excluded from commits (probe_*). They pin the invariant the shipped tests don't
// cover: on a terminal that draws emoji one cell wide, cellWidth must shrink emoji
// (and ONLY emoji — not CJK/kana/Hangul), and the status-panel box must keep every
// row the same terminal width so the right border stays plumb.

// restoreWidthState snapshots and restores the package-level width flags so a probe
// test can flip emojiNarrow without leaking into others.
func restoreWidthState(t *testing.T) {
	t.Helper()
	en := emojiNarrow
	t.Cleanup(func() { setEmojiNarrow(en) })
}

func TestCellWidth_EmojiNarrow(t *testing.T) {
	restoreWidthState(t)

	setEmojiNarrow(false)
	if got := cellWidth("🚀"); got != 2 {
		t.Fatalf("emoji wide: cellWidth(🚀)=%d, want 2", got)
	}

	setEmojiNarrow(true)
	cases := map[string]int{
		"🚀":  1, // single emoji shrinks 2→1
		"한":  2, // Hangul syllable stays wide
		"あ":  2, // Hiragana stays wide
		"漢":  2, // CJK ideograph stays wide
		"Ａ":  2, // fullwidth Latin stays wide
		"★":  1, // ambiguous star: ansi measures 1, so no emoji correction, unchanged
		"a":  1, // ascii unchanged
		"🚀x": 2, // emoji(1) + ascii(1)
		"🚀🚀": 2, // two emoji: 2→? each -1 → 2
	}
	for s, want := range cases {
		if got := cellWidth(s); got != want {
			t.Errorf("emojiNarrow cellWidth(%q)=%d, want %d", s, got, want)
		}
	}
}

// The per-grapheme cache must hold the classification and be wiped when the flag is
// re-decided, so a toggled probe can't leave a stale entry.
func TestGraphemeWidthCache_FillAndInvalidate(t *testing.T) {
	restoreWidthState(t)

	setEmojiNarrow(true)
	_ = cellWidth("🚀")
	if d, ok := graphemeWidthCache["🚀"]; !ok || d != -1 {
		t.Fatalf("cache miss/wrong for 🚀: %d ok=%v", d, ok)
	}
	if cellWidth("🚀") != 1 { // second call hits the cache, same answer
		t.Fatalf("cached cellWidth(🚀) inconsistent")
	}
	setEmojiNarrow(false) // must clear the cache
	if len(graphemeWidthCache) != 0 {
		t.Fatalf("setEmojiNarrow did not clear cache: %v", graphemeWidthCache)
	}
	if cellWidth("🚀") != 2 {
		t.Fatalf("after wide flip cellWidth(🚀) != 2")
	}
}

// Regression: a keycap emoji ("1️⃣" = '1' + VS16 + U+20E3) and the bare ascii "1"
// share a first rune but different widths. A base-rune cache key let the keycap
// poison the bare digit (both measured one cell short). Keying by full cluster keeps
// them independent — the bare "1" must stay width 1 even after the keycap is cached.
func TestEmojiWidth_KeycapNoBaseRuneCollision(t *testing.T) {
	restoreWidthState(t)
	setEmojiNarrow(true)

	if got := cellWidth("1️⃣"); got != 1 { // keycap: ansi 2 → narrow 1
		t.Fatalf("cellWidth(keycap 1️⃣)=%d, want 1", got)
	}
	if got := cellWidth("1"); got != 1 { // bare digit must be unaffected
		t.Fatalf("bare cellWidth(1)=%d, want 1 (keycap poisoned the cache)", got)
	}
	if got := cellWidth("123"); got != 3 {
		t.Fatalf("cellWidth(123)=%d, want 3", got)
	}
}

// Korean-facing regression: Hangul compatibility jamo and enclosed-CJK glyphs are
// stable-wide (terminals draw them 2 cells) and must NEVER be shrunk by the
// emoji-narrow correction, even though a block-denylist classifier missed them.
func TestEmojiWidth_StableWideNotShrunk(t *testing.T) {
	restoreWidthState(t)
	setEmojiNarrow(true)
	for _, s := range []string{"ㄱ" /*U+3131 compat jamo*/, "㊙" /*U+329D enclosed*/, "ㅏ" /*U+314F*/, "半" /*U+534A*/} {
		if got := cellWidth(s); got != 2 {
			t.Errorf("stable-wide cellWidth(%q)=%d, want 2 (mis-shrunk)", s, got)
		}
	}
}

// The real bug: an emoji in a panel row must not distort the right border. Assert
// every rendered box line occupies exactly `content` terminal cells (per cellWidth),
// both when the terminal is emoji-narrow and when it isn't.
func TestRoundedBox_UniformWidthWithEmoji(t *testing.T) {
	restoreWidthState(t)
	const content = 40
	body := strings.Join([]string{
		"Plan  1/3",
		"☐ 🚀 launch the parser",
		"◐ plain ascii row",
		"", // separator
		"✓ 한글 태스크 🚀 완료",
	}, "\n")

	for _, narrow := range []bool{false, true} {
		setEmojiNarrow(narrow)
		box := roundedBox(body, content)
		for i, line := range strings.Split(box, "\n") {
			if w := cellWidth(line); w != content {
				t.Errorf("narrow=%v line %d cellWidth=%d, want %d: %q", narrow, i, w, content, line)
			}
		}
	}
}
