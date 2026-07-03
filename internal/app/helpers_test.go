package app

import (
	"bytes"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

// canonicalArgs normalizes JSON so logically identical args (different key order
// or whitespace) fingerprint equally; non-JSON is returned verbatim.
func TestCanonicalArgs(t *testing.T) {
	a := canonicalArgs(json.RawMessage(`{"b":1,"a":2}`))
	b := canonicalArgs(json.RawMessage(`{ "a": 2, "b": 1 }`))
	if a != b {
		t.Errorf("reordered/spaced JSON should canonicalize equally: %q vs %q", a, b)
	}
	// Invalid JSON is passed through unchanged.
	if got := canonicalArgs(json.RawMessage(`not json`)); got != "not json" {
		t.Errorf("invalid JSON = %q, want passthrough", got)
	}
}

// runGuard blocks a tool call once it repeats past repeatLimit and reports the
// run as stuck only after enough blocked repeats accumulate.
func TestRunGuard(t *testing.T) {
	g := newRunGuard()
	args := json.RawMessage(`{"x":1}`)
	// First repeatLimit identical calls are allowed; the next is blocked.
	for i := 1; i <= repeatLimit; i++ {
		if block, n, _ := g.check("bash", args); block || n != i {
			t.Fatalf("call %d: block=%v n=%d, want allowed", i, block, n)
		}
	}
	if block, n, _ := g.check("bash", args); !block || n != repeatLimit+1 {
		t.Fatalf("over-limit call: block=%v n=%d, want blocked", block, n)
	}
	// A different fingerprint has its own independent counter.
	if block, _, _ := g.check("bash", json.RawMessage(`{"x":2}`)); block {
		t.Error("distinct args should not be blocked")
	}
	// A real file mutation bumps the epoch, resetting repeat counts: the same call that
	// was just blocked is allowed again, since something changed (real progress).
	g.mutated("main.go", "v1")
	if block, n, _ := g.check("bash", args); block || n != 1 {
		t.Errorf("after a mutation the repeated call should be allowed afresh: block=%v n=%d", block, n)
	}
	// The cached result of a call is echoed back when a later repeat is blocked.
	g2 := newRunGuard()
	a2 := json.RawMessage(`{"cmd":"go test"}`)
	_, _, fp := g2.check("bash", a2)
	g2.record(fp, "FAIL: 1 test failed")
	g2.check("bash", a2) // 2nd (still allowed)
	if block, _, fp3 := g2.check("bash", a2); !block || g2.lastResult(fp3) != "FAIL: 1 test failed" {
		t.Errorf("blocked repeat should expose the cached earlier result, got block=%v last=%q", block, g2.lastResult(fp3))
	}
	// Not stuck yet; force enough blocked repeats to cross blockedBudget.
	if g.stuck() != "" {
		t.Error("should not be stuck after a single block")
	}
	for g.blocked < blockedBudget {
		g.check("bash", args)
	}
	if g.stuck() != "repeat" {
		t.Error("should be stuck (repeat) once blockedBudget is reached")
	}
}

// capToolResult bounds a single result so one giant output can't overflow the context,
// truncating on a rune boundary with a note, and leaves under-cap content untouched.
func TestCapToolResult(t *testing.T) {
	small := []byte("hello")
	if got := capToolResult(small); string(got) != "hello" {
		t.Errorf("under-cap content must be unchanged, got %q", got)
	}
	big := bytes.Repeat([]byte("a"), toolResultCap+5000)
	got := capToolResult(big)
	if len(got) >= len(big) {
		t.Errorf("over-cap content should shrink: got %d, orig %d", len(got), len(big))
	}
	if !strings.Contains(string(got), "output truncated") || !utf8.Valid(got) {
		t.Errorf("truncated result should carry the marker and stay valid UTF-8")
	}
	// A multibyte rune straddling the cut must not be split (result stays valid UTF-8).
	multi := bytes.Repeat([]byte("가"), toolResultCap) // 3 bytes each → crosses the cap mid-rune
	if !utf8.Valid(capToolResult(multi)) {
		t.Error("rune-boundary truncation produced invalid UTF-8")
	}
}

// An IDEMPOTENT mutation (writing identical content to the same path) is not progress,
// so it must NOT bump the epoch — otherwise a write-the-same-thing loop would reset its
// own counts forever and never be blocked.
func TestRunGuardIdempotentMutationStillBlocks(t *testing.T) {
	g := newRunGuard()
	w := json.RawMessage(`{"path":"a.txt","content":"same"}`)
	sig := canonicalArgs(w)
	// The first write to the path is a real change → bumps the epoch (file created/modified).
	g.check("write", w)
	g.mutated("a.txt", sig)
	// Further identical writes do NOT bump the epoch, so they accumulate at one epoch.
	for i := 1; i <= repeatLimit; i++ {
		if block, _, _ := g.check("write", w); block {
			t.Fatalf("identical write %d at the stable epoch should still be allowed", i)
		}
		g.mutated("a.txt", sig) // identical content → no real change → no bump
	}
	// The next identical write exceeds repeatLimit at that stable epoch → blocked, despite
	// all the (no-op) mutations.
	if block, _, _ := g.check("write", w); !block {
		t.Error("idempotent rewrite loop should still be blocked by the guard")
	}
	// A write with DIFFERENT content is real progress → bumps the epoch → allowed afresh.
	w2 := json.RawMessage(`{"path":"a.txt","content":"different"}`)
	g.mutated("a.txt", canonicalArgs(w2))
	if block, n, _ := g.check("write", w); block || n != 1 {
		t.Errorf("after a real content change the write should be allowed afresh: block=%v n=%d", block, n)
	}
}

// truncateForCouncil caps a string to n bytes on a rune boundary (so multibyte
// runes are never split) and leaves short strings untouched.
func TestTruncateForCouncil(t *testing.T) {
	if got := truncateForCouncil("short", 100); got != "short" {
		t.Errorf("under-limit changed: %q", got)
	}
	// "héllo": 'é' is two bytes (1-2). Cutting at byte 2 must back off to byte 1.
	got := truncateForCouncil("héllo", 2)
	if !strings.HasPrefix(got, "h") || !strings.HasSuffix(got, "[diff truncated]") {
		t.Errorf("truncateForCouncil split a rune or lost marker: %q", got)
	}
	if !utf8.ValidString(got) {
		t.Errorf("result is not valid UTF-8: %q", got)
	}
}

// tailForCouncil keeps the last n bytes on a rune boundary, since failing output
// is most useful at the end.
func TestTailForCouncil(t *testing.T) {
	if got := tailForCouncil("short", 100); got != "short" {
		t.Errorf("under-limit changed: %q", got)
	}
	got := tailForCouncil("héllo", 3)
	if !strings.HasPrefix(got, "…[earlier output truncated]") {
		t.Errorf("missing tail marker: %q", got)
	}
	if !utf8.ValidString(got) {
		t.Errorf("result is not valid UTF-8: %q", got)
	}
}

// wfShell picks the platform shell: powershell on Windows, /bin/sh elsewhere.
func TestWfShell(t *testing.T) {
	sh, args := wfShell("echo hi")
	if runtime.GOOS == "windows" {
		if sh != "powershell" || len(args) != 3 || args[2] != "echo hi" {
			t.Errorf("windows wfShell = %q %v", sh, args)
		}
		return
	}
	if sh != "/bin/sh" || len(args) != 2 || args[0] != "-c" || args[1] != "echo hi" {
		t.Errorf("posix wfShell = %q %v", sh, args)
	}
}

// oneLineHint collapses newlines and runs of whitespace into single spaces.
func TestOneLineHint(t *testing.T) {
	if got := oneLineHint("a\n  b\t c\n"); got != "a b c" {
		t.Errorf("oneLineHint = %q, want \"a b c\"", got)
	}
	if got := oneLineHint("   "); got != "" {
		t.Errorf("all-whitespace → %q, want empty", got)
	}
}

// orDefault returns def only when the string is empty.
func TestOrDefault(t *testing.T) {
	if got := orDefault("x", "def"); got != "x" {
		t.Errorf("orDefault(x) = %q", got)
	}
	if got := orDefault("", "def"); got != "def" {
		t.Errorf("orDefault(empty) = %q", got)
	}
}

// firstLine returns the first line trimmed and rune-capped to n, with sentinels
// for empty input.
func TestFirstLine(t *testing.T) {
	if got := firstLine("  hello\nworld", 100); got != "hello" {
		t.Errorf("firstLine = %q, want hello", got)
	}
	if got := firstLine("abcdef", 3); got != "abc…" {
		t.Errorf("rune cap = %q, want abc…", got)
	}
	if got := firstLine("   ", 10); got != "(empty)" {
		t.Errorf("blank → %q, want (empty)", got)
	}
}

// plural renders singular for 1 and an "-s" plural otherwise.
func TestPlural(t *testing.T) {
	if got := plural(1, "step"); got != "1 step" {
		t.Errorf("plural(1) = %q", got)
	}
	if got := plural(3, "step"); got != "3 steps" {
		t.Errorf("plural(3) = %q", got)
	}
	if got := plural(0, "step"); got != "0 steps" {
		t.Errorf("plural(0) = %q", got)
	}
}

// short truncates a session ID to its first 10 chars; orDash shows "—" for empty.
func TestShortAndOrDash(t *testing.T) {
	if got := short("0123456789ABCDEF"); got != "0123456789" {
		t.Errorf("short(long) = %q", got)
	}
	if got := short("abc"); got != "abc" {
		t.Errorf("short(short) = %q", got)
	}
	if got := orDash(""); got != "—" {
		t.Errorf("orDash(empty) = %q, want —", got)
	}
	if got := orDash("x"); got != "x" {
		t.Errorf("orDash(x) = %q", got)
	}
}

// firstBalancedObject extracts the first brace-balanced JSON object, ignoring
// braces inside string literals and respecting escapes.
func TestFirstBalancedObject(t *testing.T) {
	if got := firstBalancedObject(`prefix {"a":{"b":1}} suffix`); got != `{"a":{"b":1}}` {
		t.Errorf("nested = %q", got)
	}
	// A brace inside a string literal must not unbalance the scan.
	if got := firstBalancedObject(`{"s":"has } brace"}`); got != `{"s":"has } brace"}` {
		t.Errorf("string brace = %q", got)
	}
	// An escaped quote inside a string keeps the parser in-string.
	if got := firstBalancedObject(`{"s":"a\"}b"}`); got != `{"s":"a\"}b"}` {
		t.Errorf("escaped quote = %q", got)
	}
	if got := firstBalancedObject("no object here"); got != "" {
		t.Errorf("none → %q, want empty", got)
	}
}

// truncateOutput caps long output with a marker and leaves short output alone.
func TestTruncateOutput(t *testing.T) {
	if got := truncateOutput("short", 100); got != "short" {
		t.Errorf("under-limit changed: %q", got)
	}
	got := truncateOutput("abcdefgh", 3)
	if got != "abc\n…(truncated)" {
		t.Errorf("truncateOutput = %q", got)
	}
}

// itoa formats int64s including zero and negatives, matching strconv semantics.
func TestItoa(t *testing.T) {
	for _, n := range []int64{0, 7, -7, 12345, -98765} {
		if got, want := itoa(n), fmtInt(n); got != want {
			t.Errorf("itoa(%d) = %q, want %q", n, got, want)
		}
	}
}

func fmtInt(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var digs []byte
	for n > 0 {
		digs = append([]byte{byte('0' + n%10)}, digs...)
		n /= 10
	}
	if neg {
		return "-" + string(digs)
	}
	return string(digs)
}
