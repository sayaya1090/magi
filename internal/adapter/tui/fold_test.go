package tui

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
)

// The bar fills by finished parts and never invents a share for a single part; a note with no count
// keeps the line it always had.
func TestFoldGlyphFillsByFinishedPartsOnly(t *testing.T) {
	if g := foldGlyph(nil); g != "" {
		t.Fatalf("a note with no count grew a bar: %q", g)
	}
	g := foldGlyph(&event.Progress{Done: 1, Total: 4})
	if strings.Count(g, "▰") != 2 || strings.Count(g, "▱") != 6 || !strings.Contains(g, "2/4") {
		t.Fatalf("1 of 4 parts: %q", g)
	}
	one := foldGlyph(&event.Progress{Total: 1, Chars: 300})
	if strings.Contains(one, "▰") || strings.Contains(one, "▱") || !strings.Contains(one, "⟳") {
		t.Fatalf("a single part invented a share: %q", one)
	}
}
