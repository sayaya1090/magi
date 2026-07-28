package app

import (
	"encoding/json"
	"strconv"
	"testing"
)

// A read loop that nudges only `limit` (same file, same offset) must not pace past the repeat
// counter: limit is dropped from the read fingerprint, so 60/65/70 collapse onto one count just as
// a byte-identical repeat would.
func TestReadLoopLimitJitterSharesOneCounter(t *testing.T) {
	g := newRunGuard()
	n := 0
	read := func(limit int) {
		raw := json.RawMessage(`{"path":"init.lua","offset":456,"limit":` + strconv.Itoa(limit) + `}`)
		_, n, _ = g.check("read", raw)
	}
	// Jittering the limit must not create a fresh counter: limit is dropped from the fingerprint,
	// so same-region reads collapse onto ONE count however the limit moves. Nothing is refused —
	// what the count feeds is the advisory nudge.
	for i := 0; i < 5; i++ {
		read(60 + i*5)
	}
	if n != 5 {
		t.Fatalf("jittered same-region reads must share one counter, got n=%d", n)
	}
}

// Genuine paging (advancing offset) reads DIFFERENT heads and is real forward motion,
// so it must never be treated as a repeat, no matter how many pages.
func TestReadPagingNotBlocked(t *testing.T) {
	g := newRunGuard()
	for _, off := range []int{1, 200, 400, 600, 800} {
		raw := json.RawMessage(`{"path":"big.go","offset":` + strconv.Itoa(off) + `,"limit":200}`)
		if block, _, _ := g.check("read", raw); block {
			t.Fatalf("paging read at offset %d was blocked; distinct offsets must stay distinct", off)
		}
	}
}

// The limit-drop normalization is read-specific: other tools keep full-args fingerprints,
// so a differing argument still counts as a distinct call (no collapse, no false block).
func TestNonReadKeepsFullArgs(t *testing.T) {
	g := newRunGuard()
	for i := 0; i < 5; i++ {
		raw := json.RawMessage(`{"cmd":"ls","n":` + strconv.Itoa(i) + `}`)
		if block, _, _ := g.check("bash", raw); block {
			t.Fatalf("distinct bash args (n=%d) must not be treated as a repeat", i)
		}
	}
}
