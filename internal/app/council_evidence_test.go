package app

import "testing"

// normEq compares two strings for equality after collapsing every run of whitespace to a single space
// (and trimming) — it is what the termination gate's idle-resubmit short-circuit uses to decide the
// agent reprinted "the same answer". A wrong normalization would either finish UNVERIFIED prematurely
// (false equal) or burn a council round re-deliberating an identical reply (false unequal), so lock it.
func TestClipLine(t *testing.T) {
	if got := clipLine("short", 10); got != "short" {
		t.Errorf("clipLine under the limit must be unchanged, got %q", got)
	}
	if got := clipLine("hello", 5); got != "hello" {
		t.Errorf("clipLine at exactly the limit must be unchanged, got %q", got)
	}
	if got := clipLine("hello world", 5); got != "hello…" {
		t.Errorf("clipLine over the limit must clip + ellipsis, got %q", got)
	}
	// "héllo": h(1 byte) é(2 bytes: 0xC3 0xA9) l l o. Cutting at byte 2 lands inside é, so it must back
	// up to byte 1 (the rune boundary) → "h…", never a split "h\xC3…".
	if got := clipLine("héllo", 2); got != "h…" {
		t.Errorf("clipLine must not split a multibyte rune, got %q", got)
	}
}
