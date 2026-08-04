package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A plan is a list of statuses, and raw arguments are the worst way to read one. The transcript
// used to flatten the same JSON the right panel turns into ticked lines into a single clipped
// preview. Render it the way the panel does, so the two agree about what the plan says.
func TestATodoCallRendersAsAChecklist(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.width, m.height = 90, 40

	declared := `{"todos":[{"content":"read HACKING.adoc","status":"completed"},` +
		`{"content":"find the sweep bug","status":"in_progress"},` +
		`{"content":"run the testsuite","status":"pending"}]}`
	// Models emit the array wrapped in a STRING often enough that magi's tool parsing tolerates
	// it; the renderer has to as well, or it falls back to raw arguments on exactly the calls that
	// already looked wrong.
	wrapped := `{"todos":"[{\"content\":\"read HACKING.adoc\",\"status\":\"completed\"},` +
		`{\"content\":\"find the sweep bug\",\"status\":\"in_progress\"},` +
		`{\"content\":\"run the testsuite\",\"status\":\"pending\"}]"}`

	// Both spellings: the transcript records the name the MODEL used, so an aliased `todo_write`
	// must render the same as `todowrite` — keying on the name would leave the alias raw.
	for _, c := range []struct{ name, args string }{
		{"todowrite", declared}, {"todowrite", wrapped}, {"todo_write", declared},
	} {
		out := ansi.Strip(m.renderBlock(block{kind: blockToolCall, name: c.name, args: c.args, done: true, ok: true}))
		if strings.Contains(out, `"status"`) || strings.Contains(out, "content") {
			t.Errorf("%s(%.20s…) still shows raw arguments:\n%s", c.name, c.args, out)
		}
		for _, glyph := range []string{"✓ read HACKING.adoc", "◐ find the sweep bug", "☐ run the testsuite"} {
			if !strings.Contains(out, glyph) {
				t.Errorf("%s: missing %q:\n%s", c.name, glyph, out)
			}
		}
		if !strings.Contains(out, "1/3") {
			t.Errorf("%s: the head should carry done/total:\n%s", c.name, out)
		}
	}

	// An empty plan is a head line and nothing else — no stray blank body.
	out := ansi.Strip(m.renderBlock(block{kind: blockToolCall, name: "todowrite", args: `{"todos":[]}`}))
	if n := strings.Count(strings.TrimRight(out, "\n"), "\n"); n != 0 {
		t.Errorf("an empty plan should render one line, got %d:\n%s", n+1, out)
	}

	// A tool that is not a plan is untouched.
	out = ansi.Strip(m.renderBlock(block{kind: blockToolCall, name: "read", args: `{"path":"/a.go"}`}))
	if !strings.Contains(out, "/a.go") {
		t.Errorf("an ordinary call lost its argument preview:\n%s", out)
	}
}

// Every line of a tool call carries a painted gutter, so the call reads as one section instead of
// loose lines mixed into the conversation — and it costs the same two columns indent() used, so
// nothing below it shifts.
func TestAToolCallIsOneVisuallyGroupedSection(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.width, m.height = 90, 40
	raw := m.renderBlock(block{kind: blockToolCall, name: "bash",
		args: `{"command":"go test ./..."}`, result: "exit 0\noutput: ok", done: true, ok: true})
	lines := strings.Split(raw, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected a head and a body to group, got:\n%s", ansi.Strip(raw))
	}
	for i, ln := range lines {
		if !strings.HasPrefix(ln, "\x1b[") {
			t.Errorf("line %d has no painted gutter: %q", i, ln)
		}
		if !strings.HasPrefix(ansi.Strip(ln), "  ") {
			t.Errorf("line %d does not keep the two-column indent: %q", i, ansi.Strip(ln))
		}
	}
	// Width-neutral: the gutter is a painted SPACE, not a box glyph. `│`/`▌` are
	// East-Asian-ambiguous and a CJK terminal may give them two cells, shifting every line under
	// them and breaking hit-tests that measure the plain text.
	for _, glyph := range []string{"│", "▌", "┃", "┆"} {
		if strings.Contains(raw, glyph) {
			t.Errorf("the gutter uses %q, whose width is ambiguous in a CJK terminal", glyph)
		}
	}
}
