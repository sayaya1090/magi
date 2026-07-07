package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"

	"github.com/sayaya1090/magi/internal/port"
)

// A file stored NFD (as macOS frequently does with Hangul) must still match an `old`
// the model wrote NFC — visually identical, byte-different. Before the normalization
// tier this failed with "not found", the reported "한글 포함 시 edit 실패".
func TestEditToleratesNFDvsNFC(t *testing.T) {
	nfc := "// 함수 정의\n"
	seed := "package x\n" + norm.NFD.String(nfc) + "func F() {}\n"
	if strings.Contains(seed, nfc) {
		t.Fatal("test setup: seed should be NFD and NOT contain the precomposed old byte-for-byte")
	}
	got, msg, isErr := runEdit(t, seed, editArgs{Old: nfc, New: "// 정의 변경\n"})
	if isErr {
		t.Fatalf("NFD/NFC edit should match, got error: %q", msg)
	}
	if !strings.Contains(norm.NFC.String(got), "정의 변경") || strings.Contains(norm.NFC.String(got), "함수 정의") {
		t.Errorf("edit not applied: %q", got)
	}
}

// The reverse direction: an NFC file with an NFD old-string also matches.
func TestEditToleratesNFCvsNFD(t *testing.T) {
	seed := "package x\n// 함수 정의\nfunc F() {}\n" // precomposed on disk
	got, _, isErr := runEdit(t, seed, editArgs{Old: norm.NFD.String("// 함수 정의\n"), New: "// 바뀜\n"})
	if isErr {
		t.Fatalf("NFC/NFD edit should match, got error")
	}
	if !strings.Contains(got, "바뀜") {
		t.Errorf("edit not applied: %q", got)
	}
}

// multiedit delegates to applyEdit, so it inherits the same NFD/NFC tolerance instead
// of the old strict byte-exact match that failed on a macOS-normalized Korean file.
func TestMultiEditToleratesNFDvsNFC(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.go")
	seed := "package x\n" + norm.NFD.String("// 첫 줄\n// 둘째 줄\n") + "func F() {}\n"
	if err := os.WriteFile(p, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	args := multiEditArgs{Path: "f.go", Edits: []editHunk{
		{Old: "// 첫 줄\n", New: "// FIRST\n"},   // NFC old vs NFD file
		{Old: "// 둘째 줄\n", New: "// SECOND\n"}, // second hunk also NFC
	}}
	raw, _ := json.Marshal(args)
	res, _ := MultiEdit{}.Execute(context.Background(), raw, port.ToolEnv{Workdir: dir})
	if res.IsError {
		var msg string
		_ = json.Unmarshal(res.Content, &msg)
		t.Fatalf("multiedit should match NFD file with NFC olds: %q", msg)
	}
	b, _ := os.ReadFile(p)
	got := string(b)
	if !strings.Contains(got, "FIRST") || !strings.Contains(got, "SECOND") {
		t.Errorf("multiedit hunks not applied: %q", got)
	}
}
