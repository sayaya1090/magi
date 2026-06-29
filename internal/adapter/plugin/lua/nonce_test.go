package lua

import (
	"strings"
	"testing"
)

// magi.nonce returns a hex string of the requested byte length (default 16 → 32 hex
// chars) and is unpredictable across calls — unlike the sandbox's deterministically
// seeded math.random, so plugins can use it for OAuth/PKCE state and the like.
func TestNonce(t *testing.T) {
	out, err := loadOut(t,
		`name="n"`+"\n"+`capabilities=["tool"]`,
		`local a = magi.nonce()
local b = magi.nonce()
local c = magi.nonce(8)
magi.log("alen=" .. #a .. " clen=" .. #c .. " distinct=" .. tostring(a ~= b) .. " hex=" .. tostring(a:match("^[0-9a-f]+$") ~= nil))`,
	)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, want := range []string{"alen=32", "clen=16", "distinct=true", "hex=true"} {
		if !strings.Contains(out, want) {
			t.Errorf("nonce: missing %q in %q", want, out)
		}
	}
}

// A non-positive or oversized length falls back to a sane bound rather than erroring.
func TestNonceBounds(t *testing.T) {
	out, err := loadOut(t,
		`name="n"`+"\n"+`capabilities=["tool"]`,
		`magi.log("zero=" .. #magi.nonce(0) .. " big=" .. #magi.nonce(9999))`,
	)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// 0 → default 16 bytes (32 hex); 9999 → capped at 256 bytes (512 hex).
	if !strings.Contains(out, "zero=32") || !strings.Contains(out, "big=512") {
		t.Errorf("nonce bounds wrong: %q", out)
	}
}
