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
	// Identical calls climb one counter and are NEVER refused — the count is what the advisory
	// nudge reads.
	for i := 1; i <= repeatLimit+1; i++ {
		if block, n, _ := g.check("bash", args); block || n != i {
			t.Fatalf("call %d: block=%v n=%d, want allowed with n=%d", i, block, n, i)
		}
	}
	// A different fingerprint has its own independent counter.
	if _, n, _ := g.check("bash", json.RawMessage(`{"x":2}`)); n != 1 {
		t.Errorf("distinct args need their own counter, got n=%d", n)
	}
	// A real file mutation bumps the epoch, resetting repeat counts: the same call that
	// was just blocked is allowed again, since something changed (real progress).
	g.mutated("main.go", "v1")
	if block, n, _ := g.check("bash", args); block || n != 1 {
		t.Errorf("after a mutation the repeated call should be allowed afresh: block=%v n=%d", block, n)
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
// so it must NOT bump the epoch. Nor may a mutation of some OTHER path reset the count: a
// file-modifying call's fingerprint tracks the last mutation of ITS OWN path. Nor may a mutation of some OTHER path reset them:
// a file-modifying call's fingerprint tracks the last mutation of ITS OWN path, so a scratch
// redirect between two identical writes no longer hands the second a fresh count. Nor may a mutation of some OTHER path reset them:
// a file-modifying call's fingerprint tracks the last mutation of ITS OWN path, so a scratch
// redirect between two identical writes no longer hands the second a fresh count.
func TestRunGuardIdempotentMutationStillBlocks(t *testing.T) {
	g := newRunGuard()
	w := json.RawMessage(`{"path":"a.txt","content":"same"}`)
	sig := canonicalArgs(w)
	// The first write to the path is a real change → bumps the epoch (file created/modified).
	g.check("write", w)
	g.mutated("a.txt", sig)
	// Further identical writes do NOT bump the epoch, so they accumulate on the first one's count
	// instead of each starting fresh — an idempotent rewrite loop stays visible as one repeat.
	for i := 1; i <= repeatLimit; i++ {
		g.check("write", w)
		g.mutated("a.txt", sig) // identical content → no real change → no bump
	}
	if _, n, _ := g.check("write", w); n <= repeatLimit {
		t.Errorf("idempotent rewrites must keep climbing one counter, got n=%d", n)
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
