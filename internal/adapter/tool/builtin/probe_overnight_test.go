package builtin

// Overnight exploratory probe (2026-07-08 session). NOT a committed test — it is
// a bug-hunting harness that drives every builtin tool through complex/edge-case
// inputs and logs the observed behavior. Run with:
//
//	MAGI_E2E_OLLAMA_BASE=disabled go test ./internal/adapter/tool/builtin/ \
//	  -run TestOvernightProbe -v > /tmp/probe.log 2>&1
//
// It never fails the build; findings are analyzed from the log. Lines tagged
// "SUSPECT" mark behavior worth a closer look.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// exec drives a tool against an explicit workdir (so multi-step fixtures like
// symlinks and edit-then-read work). Returns the decoded string payload (or the
// raw JSON if it isn't a JSON string) and isError.
func exProbe(dir string, tool port.Tool, args any) (string, bool) {
	raw, _ := json.Marshal(args)
	res, err := tool.Execute(context.Background(), raw, port.ToolEnv{Workdir: dir})
	if err != nil {
		return fmt.Sprintf("<Execute err: %v>", err), true
	}
	var s string
	if json.Unmarshal(res.Content, &s) == nil {
		return s, res.IsError
	}
	return string(res.Content), res.IsError
}

func oneline(s string) string {
	s = strings.ReplaceAll(s, "\n", "\\n")
	if len(s) > 140 {
		s = s[:140] + "…"
	}
	return s
}

func TestOvernightProbe(t *testing.T) {
	// ---------- Family A: path-jail escape attempts ----------
	t.Run("A_pathjail", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(dir, "in.txt", "inside\n")
		// A real secret sibling OUTSIDE the workdir.
		outside := filepath.Join(filepath.Dir(dir), "SECRET_"+filepath.Base(dir)+".txt")
		os.WriteFile(outside, []byte("TOPSECRET\n"), 0o644)
		defer os.Remove(outside)
		// A symlink inside the workdir that points outside it.
		_ = os.Symlink(outside, filepath.Join(dir, "escape_link"))
		_ = os.Symlink(filepath.Dir(dir), filepath.Join(dir, "parent_link"))

		escapes := []string{
			"../" + filepath.Base(outside),
			"../../etc/passwd",
			"/etc/passwd",
			outside,
			"./../" + filepath.Base(outside),
			"in.txt/../../" + filepath.Base(outside),
			"escape_link",                           // symlink -> outside file
			"parent_link/" + filepath.Base(outside), // symlink dir -> parent
			"....//....//etc/passwd",
			"..",
			".",
			"",
			"subdir/../../" + filepath.Base(outside),
		}
		for _, p := range escapes {
			got, isErr := exProbe(dir, Read{}, readArgs{Path: p})
			leaked := strings.Contains(got, "TOPSECRET") || strings.Contains(got, "root:")
			tag := "ok-denied"
			if leaked {
				tag = "SUSPECT-LEAK"
			} else if !isErr {
				tag = "read-ok(no-leak)"
			}
			t.Logf("[A read ] path=%-40q isErr=%-5v %s :: %s", p, isErr, tag, oneline(got))
		}
		// Write escapes: try to plant a file outside the jail.
		for _, p := range []string{"../ESCAPED_WRITE.txt", "/tmp/ESCAPED_WRITE.txt", "escape_link", "parent_link/ESCAPED.txt"} {
			got, isErr := exProbe(dir, Write{}, writeArgs{Path: p, Content: "x"})
			t.Logf("[A write] path=%-30q isErr=%-5v :: %s", p, isErr, oneline(got))
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "ESCAPED_WRITE.txt")); err == nil {
			t.Logf("[A write] SUSPECT-ESCAPE: ../ESCAPED_WRITE.txt landed outside the jail")
		}
	})

	// ---------- Family B: read edge cases ----------
	t.Run("B_read", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(dir, "nonl.txt", "no trailing newline")
		writeFile(dir, "crlf.txt", "a\r\nb\r\nc\r\n")
		writeFile(dir, "empty.txt", "")
		writeFile(dir, "nl_only.txt", "\n\n\n")
		writeFile(dir, "cjk.txt", "한글\t가나다\n日本語\n")
		os.WriteFile(filepath.Join(dir, "bin.dat"), []byte{0x00, 0x01, 0x02, 'a', 'b', 0x00}, 0o644)
		big := strings.Repeat("line\n", 5000)
		writeFile(dir, "big.txt", big)
		longline := strings.Repeat("x", 200000) + "\n"
		writeFile(dir, "longline.txt", longline)

		cases := []readArgs{
			{Path: "nonl.txt"},
			{Path: "crlf.txt"},
			{Path: "empty.txt"},
			{Path: "nl_only.txt"},
			{Path: "cjk.txt"},
			{Path: "bin.dat"},
			{Path: "big.txt"},
			{Path: "big.txt", Offset: 4999, Limit: 10},
			{Path: "big.txt", Offset: 99999, Limit: 5}, // past EOF (N6 area)
			{Path: "big.txt", Offset: -5, Limit: 3},    // negative offset
			{Path: "big.txt", Offset: 3, Limit: 0},     // zero limit
			{Path: "big.txt", Offset: 3, Limit: -1},    // negative limit
			{Path: "longline.txt"},
		}
		for _, c := range cases {
			got, isErr := exProbe(dir, Read{}, c)
			t.Logf("[B read ] %+v isErr=%-5v len=%d :: %s", c, isErr, len(got), oneline(got))
		}
	})

	// ---------- Family C: write edge cases ----------
	t.Run("C_write", func(t *testing.T) {
		dir := t.TempDir()
		os.Mkdir(filepath.Join(dir, "adir"), 0o755)
		cases := []struct {
			name string
			a    writeArgs
		}{
			{"nested-mkdir", writeArgs{Path: "a/b/c/deep.txt", Content: "deep"}},
			{"over-dir", writeArgs{Path: "adir", Content: "x"}},
			{"empty-content", writeArgs{Path: "empty.txt", Content: ""}},
			{"control-chars", writeArgs{Path: "ctl.txt", Content: "a\x1b[31mred\x1b[0m\x07\x00b"}},
			{"crlf-content", writeArgs{Path: "crlf.txt", Content: "a\r\nb\r\n"}},
			{"trailing-space-path", writeArgs{Path: "space .txt", Content: "x"}},
			{"dotfile", writeArgs{Path: ".hidden", Content: "x"}},
		}
		for _, c := range cases {
			got, isErr := exProbe(dir, Write{}, c.a)
			t.Logf("[C write] %-20s isErr=%-5v :: %s", c.name, isErr, oneline(got))
		}
	})

	// ---------- Family D: edit / multiedit ----------
	t.Run("D_edit", func(t *testing.T) {
		mk := func() string {
			d := t.TempDir()
			writeFile(d, "f.txt", "alpha\nbeta\nbeta\ngamma\n")
			return d
		}
		editCases := []struct {
			name string
			a    editArgs
		}{
			{"not-found", editArgs{Path: "f.txt", Old: "zzz", New: "q"}},
			{"ambiguous(2x)", editArgs{Path: "f.txt", Old: "beta", New: "B"}},
			{"empty-old", editArgs{Path: "f.txt", Old: "", New: "X"}},
			{"old==new", editArgs{Path: "f.txt", Old: "alpha", New: "alpha"}},
			{"replace-all", editArgs{Path: "f.txt", Old: "beta", New: "B", ReplaceAll: true}},
			{"unique-ok", editArgs{Path: "f.txt", Old: "gamma", New: "G"}},
			{"crlf-tolerant", editArgs{Path: "f.txt", Old: "alpha\r\nbeta", New: "AB"}},
			{"trailing-ws-tolerant", editArgs{Path: "f.txt", Old: "alpha  ", New: "A"}},
			{"missing-file", editArgs{Path: "nope.txt", Old: "a", New: "b"}},
		}
		for _, c := range editCases {
			d := mk()
			got, isErr := exProbe(d, Edit{}, c.a)
			t.Logf("[D edit ] %-22s isErr=%-5v :: %s", c.name, isErr, oneline(got))
		}
		// multiedit: sequential dependency + conflict
		d := mk()
		got, isErr := exProbe(d, MultiEdit{}, multiEditArgs{Path: "f.txt", Edits: []editHunk{
			{Old: "alpha", New: "one"},
			{Old: "one", New: "1"}, // depends on the first edit's output
			{Old: "gamma", New: "3"},
		}})
		t.Logf("[D multi] sequential-dep isErr=%-5v :: %s", isErr, oneline(got))
		d = mk()
		got, isErr = exProbe(d, MultiEdit{}, multiEditArgs{Path: "f.txt", Edits: []editHunk{
			{Old: "beta", New: "B"}, // ambiguous inside a batch
		}})
		t.Logf("[D multi] ambiguous-in-batch isErr=%-5v :: %s", isErr, oneline(got))
		d = mk()
		got, isErr = exProbe(d, MultiEdit{}, multiEditArgs{Path: "f.txt", Edits: []editHunk{
			{Old: "alpha", New: "one"},
			{Old: "NOPE", New: "x"}, // second hunk fails — is the first rolled back?
		}})
		after, _ := exProbe(d, Read{}, readArgs{Path: "f.txt"})
		t.Logf("[D multi] partial-fail isErr=%-5v atomic?=%v :: %s", isErr, !strings.Contains(after, "one"), oneline(got))
	})

	// ---------- Family E: grep ----------
	t.Run("E_grep", func(t *testing.T) {
		seed := func(d string) {
			writeFile(d, "a.go", "package a\nfunc Foo() {}\n// TODO: x\n")
			writeFile(d, "b.txt", "foo\nFOO\nfOo\n")
			os.WriteFile(filepath.Join(d, "bin.dat"), []byte{0, 'f', 'o', 'o', 0}, 0o644)
		}
		dir := t.TempDir()
		seed(dir)
		cases := []grepArgs{
			{Pattern: "Foo"},
			{Pattern: "foo"},
			{Pattern: "(?i)foo"},
			{Pattern: "["},     // invalid regex
			{Pattern: "^func"}, // anchor
			{Pattern: "TODO", Glob: "*.go"},
			{Pattern: "nomatch-zzz"}, // N9: null vs []
			{Pattern: "foo", Path: "."},
			{Pattern: ".*"},                // match-everything
			{Pattern: "foo", Glob: "*.md"}, // filter excludes all
		}
		for _, c := range cases {
			got, isErr := exProbe(dir, Grep{}, c)
			t.Logf("[E grep ] %+v isErr=%-5v :: %s", c, isErr, oneline(got))
		}
	})

	// ---------- Family F: glob ----------
	t.Run("F_glob", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(dir, "x.go", "x")
		writeFile(dir, "y.GO", "y")
		writeFile(dir, "sub/z.go", "z")
		writeFile(dir, ".hidden/h.go", "h")
		_ = os.Symlink(filepath.Join(dir, "sub"), filepath.Join(dir, "sublink"))
		cases := []string{"*.go", "**/*.go", "**/*.{go,txt}", "*.[gG][oO]", "sub/*.go", ".hidden/*.go", "sublink/*.go", "**", "[", "{a,b}.go"}
		for _, p := range cases {
			got, isErr := exProbe(dir, Glob{}, globArgs{Pattern: p})
			t.Logf("[F glob ] pat=%-18q isErr=%-5v :: %s", p, isErr, oneline(got))
		}
	})

	// ---------- Family G: bash / bgproc ----------
	t.Run("G_bash", func(t *testing.T) {
		dir := t.TempDir()
		cases := []struct {
			name string
			a    bashArgs
		}{
			{"exit-nonzero", bashArgs{Command: "exit 3"}},
			{"stderr-capture", bashArgs{Command: "echo out; echo err 1>&2"}},
			{"cwd", bashArgs{Command: "pwd"}},
			{"cd-nonpersist-1", bashArgs{Command: "cd /tmp && pwd"}},
			{"cd-nonpersist-2", bashArgs{Command: "pwd"}}, // should be dir, not /tmp
			{"env-home", bashArgs{Command: "echo HOME=$HOME"}},
			{"timeout-precision", bashArgs{Command: "sleep 5", Timeout: 1}},
			{"huge-output", bashArgs{Command: "yes | head -n 200000"}},
			{"ansi-output", bashArgs{Command: "printf '\\033[31mRED\\033[0m\\007done\\n'"}},
			{"unicode", bashArgs{Command: "echo 한글 émoji 🎉"}},
			{"signal-kill", bashArgs{Command: "kill -9 $$"}},
			{"stdin-none", bashArgs{Command: "cat"}}, // no stdin — should not hang
		}
		for _, c := range cases {
			got, isErr := exProbe(dir, Bash{}, c.a)
			t.Logf("[G bash ] %-18s isErr=%-5v :: %s", c.name, isErr, oneline(got))
		}
	})

	// ---------- Family H: list ----------
	t.Run("H_list", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(dir, "a.txt", "a")
		writeFile(dir, "sub/b.txt", "b")
		writeFile(dir, ".hidden", "h")
		os.Mkdir(filepath.Join(dir, "emptydir"), 0o755)
		outside := filepath.Join(filepath.Dir(dir), "OUT_"+filepath.Base(dir))
		os.MkdirAll(outside, 0o755)
		os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("S"), 0o644)
		defer os.RemoveAll(outside)
		_ = os.Symlink(outside, filepath.Join(dir, "esc"))
		for _, p := range []string{"", ".", "sub", "emptydir", "nope", "esc", "..", "/etc", "a.txt"} {
			got, isErr := exProbe(dir, List{}, listArgs{Path: p})
			leaked := strings.Contains(got, "secret.txt")
			tag := ""
			if leaked {
				tag = "SUSPECT-LEAK"
			}
			t.Logf("[H list ] path=%-8q isErr=%-5v %s :: %s", p, isErr, tag, oneline(got))
		}
	})

	// ---------- Family J: todowrite ----------
	t.Run("J_todo", func(t *testing.T) {
		dir := t.TempDir()
		cases := []struct {
			name  string
			todos []session.Todo
		}{
			{"empty", nil},
			{"valid", []session.Todo{{Content: "a", Status: "pending"}, {Content: "b", Status: "in_progress"}}},
			{"bad-status", []session.Todo{{Content: "a", Status: "banana"}}},
			{"empty-content", []session.Todo{{Content: "", Status: "pending"}}},
			{"missing-status", []session.Todo{{Content: "a"}}},
			{"two-in-progress", []session.Todo{{Content: "a", Status: "in_progress"}, {Content: "b", Status: "in_progress"}}},
			{"dup-content", []session.Todo{{Content: "x", Status: "pending"}, {Content: "x", Status: "completed"}}},
		}
		for _, c := range cases {
			got, isErr := exProbe(dir, TodoWrite{}, todoWriteArgs{Todos: c.todos})
			t.Logf("[J todo ] %-18s isErr=%-5v :: %s", c.name, isErr, oneline(got))
		}
	})

	// ---------- Wave 11: Unicode-normalization class hunt (grep / findcontext) ----------
	// edit/multiedit were just fixed for the macOS NFD-Hangul vs model-NFC mismatch
	// (P3). This walks the sibling SEARCH surfaces: a file stored NFD, queried with the
	// NFC form the model emits. A silent miss ("no matches"/score 0) is the same class.
	t.Run("K_norm_search", func(t *testing.T) {
		dir := t.TempDir()
		nfc := "함수 정의" // precomposed (what a model types)
		nfd := norm.NFD.String(nfc)
		if nfc == nfd {
			t.Fatal("setup: NFC and NFD forms are identical — pick different text")
		}
		// File body is NFD on disk (the darwin case); the identifier is Korean.
		writeFile(dir, "kor.go", "package x\n// "+nfd+"\nfunc F() {}\n")
		// Also a Latin control that must keep matching (no regression).
		writeFile(dir, "eng.go", "package y\n// parse config here\nfunc G() {}\n")

		// grep: NFC pattern against the NFD file.
		got, isErr := exProbe(dir, Grep{}, map[string]any{"pattern": nfc})
		miss := isErr || !strings.Contains(got, "kor.go")
		t.Logf("[K grep ] nfc-pattern-vs-nfd-file isErr=%-5v miss=%-5v :: %s", isErr, miss, oneline(got))
		if miss {
			t.Logf("[K grep ] SUSPECT: grep missed an NFD file for an NFC pattern (P3 class)")
		}
		// grep control: NFD pattern SHOULD match the NFD file (sanity that content is there).
		gotD, _ := exProbe(dir, Grep{}, map[string]any{"pattern": nfd})
		t.Logf("[K grep ] nfd-pattern-vs-nfd-file (control) match=%-5v :: %s", strings.Contains(gotD, "kor.go"), oneline(gotD))
		// grep Latin control: must still match.
		gotL, _ := exProbe(dir, Grep{}, map[string]any{"pattern": "parse config"})
		t.Logf("[K grep ] latin-control match=%-5v :: %s", strings.Contains(gotL, "eng.go"), oneline(gotL))

	})

	// ---------- Wave 12: NFC class — FILENAME matching (glob tool + grep --glob) ----------
	// Content search was closed in Wave 11; the sibling surface is name matching. A file
	// whose NAME is NFD on disk, matched against the NFC glob a model types.
	t.Run("L_norm_glob", func(t *testing.T) {
		dir := t.TempDir()
		nfcName := "함수"
		nfdName := norm.NFD.String(nfcName)
		if nfcName == nfdName {
			t.Fatal("setup: NFC and NFD forms are identical")
		}
		writeFile(dir, nfdName+".go", "package x\nfunc F() {}\n")
		writeFile(dir, "plain.go", "package y\n")

		// glob tool: NFC pattern (substring + suffix) against the NFD-named file.
		got, isErr := exProbe(dir, Glob{}, map[string]any{"pattern": "*" + nfcName + "*.go"})
		miss := isErr || !strings.Contains(got, ".go\"") || !strings.Contains(got, nfdName)
		t.Logf("[L glob ] nfc-pattern-vs-nfd-name isErr=%-5v miss=%-5v :: %s", isErr, miss, oneline(got))
		if miss {
			t.Logf("[L glob ] SUSPECT: glob missed an NFD-named file for an NFC pattern (P3 class)")
		}
		// glob control: ASCII pattern must still list the Latin file.
		gotA, _ := exProbe(dir, Glob{}, map[string]any{"pattern": "*.go"})
		t.Logf("[L glob ] ascii-control plain.go=%v :: %s", strings.Contains(gotA, "plain.go"), oneline(gotA))

		// grep --glob: NFC glob filter against the NFD-named file (should still find content).
		gg, ggErr := exProbe(dir, Grep{}, map[string]any{"pattern": "func F", "glob": "*" + nfcName + "*.go"})
		ggMiss := ggErr || !strings.Contains(gg, nfdName)
		t.Logf("[L grep ] nfc-glob-vs-nfd-name isErr=%-5v miss=%-5v :: %s", ggErr, ggMiss, oneline(gg))
		if ggMiss {
			t.Logf("[L grep ] SUSPECT: grep --glob missed an NFD-named file for an NFC glob (P3 class)")
		}
	})

}
