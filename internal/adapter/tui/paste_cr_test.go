package tui

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// A bracketed paste whose line breaks arrive as CR (\r) — how terminals deliver
// Enter inside a paste — must be normalized to \n so the line count is right and
// no raw CR survives to overwrite the row on render. Regression for the
// "[#N pasted 1 lines]" + dropped-output paste bug.
func TestPasteNormalizesCRLineEndings(t *testing.T) {
	for _, tc := range []struct {
		name, sep string
	}{
		{"lone-CR", "\r"},
		{"CRLF", "\r\n"},
		{"LF", "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newPasteModel()
			parts := make([]string, 8)
			for i := range parts {
				parts[i] = "line-of-pasted-content-number-" + strconv.Itoa(i)
			}
			m.handlePaste(strings.Join(parts, tc.sep))

			val := m.ta.Value()
			num := regexp.MustCompile(`pasted (\d+) lines?`).FindStringSubmatch(val)
			if num == nil {
				t.Fatalf("expected a collapsed placeholder, got %q", val)
			}
			if n, _ := strconv.Atoi(num[1]); n != 8 {
				t.Fatalf("%s paste miscounted: says %d lines, want 8", tc.name, n)
			}
			full := m.expandPastes(val)
			if strings.ContainsAny(full, "\r") {
				t.Fatalf("%s: expanded paste still holds raw CR", tc.name)
			}
			for _, p := range parts {
				if !strings.Contains(full, p) {
					t.Fatalf("%s: expanded paste dropped %q", tc.name, p)
				}
			}
		})
	}
}
