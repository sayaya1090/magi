package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sayaya1090/magi/internal/port"
)

// TestReadDefaultLineCap: a bare read (no limit) of a file longer than
// defaultReadLines returns exactly that many numbered lines plus a "more lines"
// footer pointing at where to resume — not the whole file (O5).
func TestReadDefaultLineCap(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	total := defaultReadLines + 500
	for i := 1; i <= total; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Read{}.Execute(context.Background(), json.RawMessage(`{"path":"big.txt"}`), port.ToolEnv{Workdir: dir})
	if err != nil || res.IsError {
		t.Fatalf("read errored: %v %s", err, res.Content)
	}
	out := string(res.Content)
	// The window ends at defaultReadLines; the next line must NOT appear.
	if !strings.Contains(out, fmt.Sprintf("line %d", defaultReadLines)) {
		t.Errorf("expected last shown line %d present", defaultReadLines)
	}
	if strings.Contains(out, fmt.Sprintf("line %d\n", defaultReadLines+1)) {
		t.Error("line past the default cap should not be shown")
	}
	if !strings.Contains(out, fmt.Sprintf("%d more lines", 500)) {
		t.Errorf("expected a '500 more lines' footer, got tail: %q", tail(out, 120))
	}
	if !strings.Contains(out, fmt.Sprintf("offset=%d", defaultReadLines+1)) {
		t.Errorf("footer should point at offset=%d to resume", defaultReadLines+1)
	}
}

// TestReadSmallFileNoFooter: a file within the cap is returned whole, with no
// spurious "more lines" note.
func TestReadSmallFileNoFooter(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "s.txt"), []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, _ := Read{}.Execute(context.Background(), json.RawMessage(`{"path":"s.txt"}`), port.ToolEnv{Workdir: dir})
	out := string(res.Content)
	if strings.Contains(out, "more lines") {
		t.Errorf("small file should not carry a cap footer: %q", out)
	}
	for _, want := range []string{"a", "b", "c"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing line %q in %q", want, out)
		}
	}
}

// TestReadExplicitLimitNoFooter: an explicit limit is the caller's choice, so no
// default-cap footer is appended even when it hides lines.
func TestReadExplicitLimitNoFooter(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "s.txt"), []byte("a\nb\nc\nd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, _ := Read{}.Execute(context.Background(), json.RawMessage(`{"path":"s.txt","limit":2}`), port.ToolEnv{Workdir: dir})
	out := string(res.Content)
	if strings.Contains(out, "more lines") {
		t.Errorf("explicit limit should not append the default-cap footer: %q", out)
	}
}

// TestTruncateOutHeadTail (I1): oversized output keeps both the head and the tail so a
// failure message at the end survives display truncation, and the result stays valid UTF-8.
func TestTruncateOutHeadTail(t *testing.T) {
	head := strings.Repeat("H", 40*1024)
	tailMark := "FATAL: build failed at the very end"
	s := head + strings.Repeat("m", 40*1024) + tailMark
	got := truncateOut(s)
	if len(got) >= len(s) {
		t.Fatalf("expected truncation, got %d >= %d", len(got), len(s))
	}
	if !strings.HasPrefix(got, "HHHH") {
		t.Error("head not preserved")
	}
	if !strings.HasSuffix(got, tailMark) {
		t.Errorf("tail (the error) not preserved: ...%q", tail(got, 60))
	}
	if !strings.Contains(got, "bytes omitted") {
		t.Error("expected an elision marker")
	}
	if !utf8.ValidString(got) {
		t.Error("result must be valid UTF-8")
	}
}

// TestReadHeadTailBounds (I1): a file far larger than cap is read down to ~cap bytes
// (head+tail+marker), not fully buffered; a file within cap is returned whole.
func TestReadHeadTailBounds(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "big")
	if err := os.WriteFile(big, make([]byte, 4<<20), 0o644); err != nil { // 4 MiB of NULs
		t.Fatal(err)
	}
	got := readHeadTail(big, captureCap)
	if int64(len(got)) > captureCap+128 { // +marker slack
		t.Errorf("readHeadTail returned %d bytes, want <= ~cap %d (memory not bounded)", len(got), captureCap)
	}
	if !strings.Contains(string(got), "bytes omitted") {
		t.Error("expected elision marker on an over-cap file")
	}
	small := filepath.Join(dir, "small")
	if err := os.WriteFile(small, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readHeadTail(small, captureCap); string(got) != "hello" {
		t.Errorf("small file = %q, want whole content", got)
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// TestRotateIfHuge: once a background log exceeds hardLogCap it is truncated and
// the read offset rewound, bounding on-disk size (O2).
func TestRotateIfHuge(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "magi-bg-*.log")
	if err != nil {
		t.Fatal(err)
	}
	// Write just over the cap.
	if err := f.Truncate(hardLogCap + 4096); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	p := &bgProc{logPath: f.Name(), read: 1234}

	dropped := rotateIfHuge(p)
	if dropped < hardLogCap {
		t.Errorf("dropped=%d, want >= cap %d", dropped, hardLogCap)
	}
	if fi, _ := os.Stat(p.logPath); fi.Size() != 0 {
		t.Errorf("log size after rotate = %d, want 0", fi.Size())
	}
	if p.read != 0 {
		t.Errorf("read offset after rotate = %d, want 0", p.read)
	}
	// Under the cap: no-op.
	if got := rotateIfHuge(p); got != 0 {
		t.Errorf("under-cap rotate returned %d, want 0", got)
	}
}

// TestBashOutputPagesLargeBurst (I3): a single burst larger than maxBgBuf must surface in
// full across successive readLogSince calls — byte-for-byte, with the offset advancing by
// exactly the bytes returned and never skipping. This pins the F2 fix (read-advance ==
// displayed bytes), whose earlier form consumed the 30KB–256KB middle of a burst silently.
func TestBashOutputPagesLargeBurst(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "burst.log")
	// A 100KB burst of varied bytes so a skipped/duplicated window is detectable.
	want := make([]byte, 100*1024)
	for i := range want {
		want[i] = byte('!' + (i % 90)) // printable, position-dependent
	}
	if err := os.WriteFile(logPath, want, 0o644); err != nil {
		t.Fatal(err)
	}
	var got []byte
	since := 0
	for iter := 0; iter < 100; iter++ {
		text, next := readLogSince(logPath, since)
		if text == "" {
			break // drained
		}
		if next != since+len(text) {
			t.Fatalf("offset skipped: since=%d next=%d len=%d", since, next, len(text))
		}
		if len(text) > maxBgBuf {
			t.Fatalf("one read returned %d bytes, exceeds maxBgBuf %d", len(text), maxBgBuf)
		}
		got = append(got, text...)
		since = next
	}
	if len(got) != len(want) {
		t.Fatalf("reconstructed %d bytes, want %d", len(got), len(want))
	}
	if string(got) != string(want) {
		t.Fatal("reconstructed burst does not match byte-for-byte (F2 gap)")
	}
}

// TestSweepStaleTempLogs: only magi's own *.log temp files older than the age
// threshold are removed; fresh ones and unrelated files are kept.
func TestSweepStaleTempLogs(t *testing.T) {
	// Point the temp dir at an isolated location for this test.
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	if os.TempDir() != dir {
		t.Skipf("TMPDIR override not honored on this platform (got %s)", os.TempDir())
	}
	old := filepath.Join(dir, "magi-bg-stale.log")
	fresh := filepath.Join(dir, "magi-bash-fresh.log")
	other := filepath.Join(dir, "unrelated.log")
	for _, p := range []string{old, fresh, other} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Age the stale one past the threshold.
	staleTime := time.Now().Add(-staleTempLogAge - time.Hour)
	_ = os.Chtimes(old, staleTime, staleTime)

	SweepStaleTempLogs()

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("stale magi log should have been swept")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("fresh magi log should be kept")
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("unrelated file should be kept")
	}
}
