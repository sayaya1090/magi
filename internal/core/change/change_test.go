package change

import (
	"strings"
	"testing"
)

func TestLineDiff(t *testing.T) {
	// A one-line change keeps surrounding lines as context and marks the swap.
	old := "#include <stdio.h>\n\nint main() {\n    printf(\"Hello, World!\\n\");\n    return 0;\n}"
	neu := "#include <stdio.h>\n\nint main() {\n    printf(\"Hello, World!!\\n\");\n    return 0;\n}"
	got := LineDiff(old, neu)
	wantHas := []string{
		" #include <stdio.h>",                 // context, space-prefixed
		"-    printf(\"Hello, World!\\n\");",  // removal
		"+    printf(\"Hello, World!!\\n\");", // addition
		" }",                                  // trailing context
	}
	for _, w := range wantHas {
		if !strings.Contains(got, w) {
			t.Errorf("LineDiff missing %q\n--- got ---\n%s", w, got)
		}
	}
}

func TestEditDiffWriteIsAllAdds(t *testing.T) {
	// A write shows its content as added lines.
	got := EditDiff("write", `{"path":"main.c","content":"line1\nline2"}`)
	if got != "+line1\n+line2" {
		t.Errorf("write diff = %q", got)
	}
	// A non-edit/write tool yields nothing.
	if d := EditDiff("bash", `{"cmd":"ls"}`); d != "" {
		t.Errorf("bash should have no diff, got %q", d)
	}
	// Unparseable args yield nothing, not a panic.
	if d := EditDiff("edit", `not json`); d != "" {
		t.Errorf("bad args should yield no diff, got %q", d)
	}
}

// A diff past maxDiffLines is capped with a summary note (exercised through LineDiff).
func TestLineDiffClamps(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 100; i++ {
		b.WriteString("line\n") // 100 identical-shaped but distinct? no — make distinct adds
	}
	old := ""
	neu := ""
	for i := 0; i < 100; i++ {
		neu += "l" + strings.Repeat("x", i) + "\n" // 100 distinct added lines
	}
	got := LineDiff(old, neu)
	lines := strings.Count(got, "\n") + 1
	if lines != maxDiffLines+1 { // 40 capped + 1 summary line
		t.Errorf("clamped diff line count = %d, want %d", lines, maxDiffLines+1)
	}
	if !strings.Contains(got, "more lines)") {
		t.Errorf("clamped diff missing truncation note:\n%s", got)
	}
}
