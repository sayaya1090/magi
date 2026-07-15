package tui

import "testing"

func TestCtxGauge(t *testing.T) {
	cases := []struct {
		name           string
		pct            float64
		tokens, window int
		want           string
	}{
		{"window known", 42, 55000, 131000, "ctx 42% · 55.0k/131.0k"},
		{"window unknown falls back to tokens", 0, 55000, 0, "ctx ~55.0k"},
		{"no data is empty", 0, 0, 131000, ""},
		{"rounds percent", 41.6, 1234, 131072, "ctx 42% · 1.2k/131.1k"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := &Model{ctxPct: c.pct, ctxTokens: c.tokens, ctxWindow: c.window}
			if got := m.ctxGauge(); got != c.want {
				t.Errorf("ctxGauge() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestGaugeSep(t *testing.T) {
	if got := gaugeSep(""); got != "" {
		t.Errorf("gaugeSep(empty) = %q, want empty", got)
	}
	if got := gaugeSep("ctx 42%"); got != " · ctx 42%" {
		t.Errorf("gaugeSep(non-empty) = %q, want %q", got, " · ctx 42%")
	}
}
