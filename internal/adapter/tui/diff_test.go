package tui

import "testing"

func TestLineDiff(t *testing.T) {
	// A one-line change keeps surrounding lines as context and marks the swap.
	old := "#include <stdio.h>\n\nint main() {\n    printf(\"Hello, World!\\n\");\n    return 0;\n}"
	neu := "#include <stdio.h>\n\nint main() {\n    printf(\"Hello, World!!\\n\");\n    return 0;\n}"
	got := lineDiff(old, neu)
	wantHas := []string{
		" #include <stdio.h>",          // context, space-prefixed
		"-    printf(\"Hello, World!\\n\");",  // removal
		"+    printf(\"Hello, World!!\\n\");", // addition
		" }",                            // trailing context
	}
	for _, w := range wantHas {
		if !contains(got, w) {
			t.Errorf("lineDiff missing %q\n--- got ---\n%s", w, got)
		}
	}
}

func TestEditDiffWriteIsAllAdds(t *testing.T) {
	// A write shows its content as added lines.
	got := editDiff("write", `{"path":"main.c","content":"line1\nline2"}`)
	if got != "+line1\n+line2" {
		t.Errorf("write diff = %q", got)
	}
	// A non-edit/write tool yields nothing.
	if d := editDiff("bash", `{"cmd":"ls"}`); d != "" {
		t.Errorf("bash should have no diff, got %q", d)
	}
	// Unparseable args yield nothing, not a panic.
	if d := editDiff("edit", `not json`); d != "" {
		t.Errorf("bad args should yield no diff, got %q", d)
	}
}

func TestClampDiffBounds(t *testing.T) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = "+x"
	}
	got := clampDiff(lines)
	if n := countLines(got); n != 41 { // 40 capped + 1 summary line
		t.Errorf("clampDiff line count = %d, want 41", n)
	}
	if !contains(got, "… (60 more lines)") {
		t.Errorf("clampDiff missing truncation note:\n%s", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func countLines(s string) int {
	n := 1
	for _, c := range s {
		if c == '\n' {
			n++
		}
	}
	return n
}
