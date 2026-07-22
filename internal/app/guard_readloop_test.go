package app

import (
	"encoding/json"
	"strconv"
	"testing"
)

// A read loop that nudges only `limit` (same file, same offset) must NOT pace past
// the exact-repeat guard: limit is dropped from the read fingerprint, so 60/65/70
// collapse onto one counter and the block engages on schedule (repeatLimit=2 → the
// 3rd same-region read blocks), just as a byte-identical repeat would.
func TestReadLoopLimitJitterBlocks(t *testing.T) {
	g := newRunGuard()
	read := func(limit int) bool {
		raw := json.RawMessage(`{"path":"init.lua","offset":456,"limit":` + strconv.Itoa(limit) + `}`)
		block, _, _ := g.check("read", raw)
		return block
	}
	// repeatLimit same-region reads (limit jittered each time) are allowed — limit is dropped from
	// the fingerprint, so they collapse onto one counter; the next one blocks on schedule.
	for i := 0; i < repeatLimit; i++ {
		if read(60 + i*5) {
			t.Fatalf("same-region read #%d must be allowed under repeatLimit=%d", i+1, repeatLimit)
		}
	}
	if !read(60 + repeatLimit*5) {
		t.Fatal("the read past repeatLimit (limit jittered) must be blocked — the fingerprint must ignore limit")
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
