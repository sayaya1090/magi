//go:build !windows

package tui

import (
	"strings"
	"testing"
)

func TestReadCPRColumn(t *testing.T) {
	cases := []struct {
		name string
		in   string
		col  int
		ok   bool
	}{
		{"narrow bar", "\x1b[1;2R", 2, true}, // │ at col1 → cursor col2 → width 1
		{"wide bar", "\x1b[1;3R", 3, true},   // │ drawn wide → cursor col3 → width 2
		{"leading noise then report", "junk\x1b[12;3R", 3, true},
		{"no report", "no cpr here", 0, false},
		{"truncated", "\x1b[1;", 0, false},
		{"missing col", "\x1b[5R", 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			col, ok := readCPRColumn(strings.NewReader(c.in))
			if col != c.col || ok != c.ok {
				t.Errorf("readCPRColumn(%q) = (%d,%v), want (%d,%v)", c.in, col, ok, c.col, c.ok)
			}
		})
	}
}

// TestProbeAmbiguousWidthNonTTY: with nil files (not a terminal) the probe must
// bail cleanly, never panicking or blocking.
func TestProbeAmbiguousWidthNonTTY(t *testing.T) {
	if w, ok := probeAmbiguousWidth(nil, nil); ok {
		t.Errorf("nil files should not probe, got (%d,true)", w)
	}
}
