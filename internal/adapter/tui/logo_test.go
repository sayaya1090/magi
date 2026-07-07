package tui

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/version"
)

// The fallback splash (modal-open case) renders the wordmark + version centered in
// its area.
func TestSplashViewCentered(t *testing.T) {
	applyTheme(true)
	const w, h = 60, 30
	rows := strings.Split(splashView(w, h), "\n")
	if len(rows) != h {
		t.Fatalf("splash height = %d, want %d", len(rows), h)
	}
	if !strings.Contains(splashView(w, h), version.String()) {
		t.Errorf("splash should include the build version %q", version.String())
	}
}

// splashCompose places the input box directly beneath the wordmark, the pair
// centered as a group, and reports a cursor that lands inside the box's first text
// cell — one row below the box's top border, past its left border+padding.
func TestSplashComposeInputUnderLogo(t *testing.T) {
	applyTheme(true)
	const vpw, h = 80, 30
	// A 3-line box: top border, one content row, bottom border.
	box := "+----------+\n|          |\n+----------+"
	content, curRow, curCol := splashCompose(vpw, h, box)
	rows := strings.Split(content, "\n")
	if len(rows) != h {
		t.Fatalf("compose height = %d, want %d", len(rows), h)
	}
	if !strings.Contains(content, version.String()) {
		t.Errorf("compose should include the wordmark version %q", version.String())
	}
	// The wordmark must appear strictly above the box.
	logoRow, boxTopRow := -1, -1
	for i, r := range rows {
		if strings.Contains(r, "+----------+") {
			boxTopRow = i
			break
		}
	}
	for i, r := range rows {
		if strings.TrimSpace(r) != "" && !strings.Contains(r, "+") && !strings.Contains(r, "|") {
			logoRow = i
			break
		}
	}
	if boxTopRow < 0 {
		t.Fatalf("box not found in composed content")
	}
	if logoRow < 0 || logoRow >= boxTopRow {
		t.Errorf("wordmark (row %d) should sit above the box top (row %d)", logoRow, boxTopRow)
	}
	// The cursor row is one past the box top border, and must land on the content row.
	if curRow != boxTopRow+1 {
		t.Errorf("cursor row = %d, want box content row %d", curRow, boxTopRow+1)
	}
	// The cursor column is inside the box interior (past its left border+padding).
	boxLeft := strings.IndexByte(rows[boxTopRow], '+')
	if curCol != boxLeft+2 {
		t.Errorf("cursor col = %d, want box interior %d", curCol, boxLeft+2)
	}
}
