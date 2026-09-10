package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/provider"
	"github.com/sayaya1090/magi/internal/prompt"
)

// renderProviders: the empty machine says what to do about it, and a serving backend renders its
// catalog with the exact line that saves a profile.
func TestRenderProviders(t *testing.T) {
	if got := renderProviders(nil); !strings.Contains(got, "no provider is serving") {
		t.Fatalf("the empty answer advises: %q", got)
	}
	got := renderProviders([]provider.Provider{{Name: "ollama", Base: "http://127.0.0.1:11434", Models: []string{"m1", "m2"}}})
	for _, want := range []string{"ollama", "· m1", "· m2", "/providers ollama <model>"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

// Under go test stdin is not a character device, and RunPrompt's refusal path is exactly what a
// headless caller hits.
func TestPromptRefusesWithoutATerminal(t *testing.T) {
	if isInteractive() {
		t.Skip("a real terminal is attached")
	}
	if _, err := RunPrompt(promptSpecEmpty()); err == nil {
		t.Fatal("no terminal, no prompt")
	}
}

// The pollers and inits are wiring, and the wiring must exist: a nil Cmd here is a screen that
// never ticks.
func TestWiringCmdsExist(t *testing.T) {
	if jobPoll() == nil || bgPoll() == nil || renderTick() == nil {
		t.Fatal("a poller that is nil never polls")
	}
	if (promptModel{}).Init() != nil {
		t.Fatal("the prompt model has no startup work")
	}
	m := &Model{}
	if m.Init() == nil {
		t.Fatal("the model's init is the whole subscription batch")
	}
	if m.fetchSuggest("fix the", 1) == nil || m.cmdProviders(nil) == nil {
		t.Fatal("the async commands are closures, and a nil closure never runs")
	}
	if got := m.fadeDebug(); !strings.Contains(got, "panes=0") {
		t.Fatalf("fadeDebug renders the live state: %q", got)
	}
	restore := configureConsole()
	restore() // the POSIX no-op keeps Run's call site platform-agnostic
}

func promptSpecEmpty() (s prompt.Spec) { return }

// The width detectors honour their environment gates without touching a terminal — the branch a
// redirected or headless run takes.
func TestWidthDetectorsHonourTheEnv(t *testing.T) {
	t.Setenv("MAGI_AMBIGUOUS_WIDTH", "narrow")
	detectAmbiguousWidth()
	t.Setenv("MAGI_AMBIGUOUS_WIDTH", "")
	t.Setenv("MAGI_DECOR_WIDTH", "narrow")
	t.Setenv("MAGI_EMOJI_WIDTH", "wide")
	detectDecorWidths()
	detectEmojiWidth()
	t.Setenv("MAGI_DECOR_WIDTH", "")
	t.Setenv("MAGI_EMOJI_WIDTH", "")
	t.Setenv("MAGI_WIDTH_PROBE", "0")
	detectAmbiguousWidth() // the probe-off gate: no terminal is touched
	detectDecorWidths()
	detectEmojiWidth()
}

// The tty probes answer ok=false on a file that is not a terminal, quickly — a redirected stdin
// must not hang startup.
func TestTTYProbesFailFastOffTerminal(t *testing.T) {
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer devnull.Close()
	sink, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	if _, ok := probeAmbiguousWidth(sink, devnull); ok {
		t.Fatal("a pipe is not a terminal")
	}
	if _, ok := probeEmojiWidth(sink, devnull); ok {
		t.Fatal("likewise for the emoji probe")
	}
	if _, ok := probeDecorWidths(sink, devnull); ok {
		t.Fatal("and the decor probe")
	}
}

// paneColor hands a role its stable hue — the same role, the same color, and two roles apart.
func TestPaneColorIsStablePerRole(t *testing.T) {
	applyTheme(true) // the palette is the theme's; before any theme there is nothing to index
	m := &Model{}
	a1, a2, b := m.paneColor("api"), m.paneColor("api"), m.paneColor("design")
	if a1 != a2 {
		t.Fatal("one role, one color")
	}
	_ = b // a different role may share a palette slot; stability is the contract, not uniqueness
}

// RunPrompt with stdin that is not a terminal: the refusal, forced rather than skipped.
func TestRunPromptRefusesAPipeStdin(t *testing.T) {
	old := os.Stdin
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	os.Stdin = f
	defer func() { os.Stdin = old }()
	if _, err := RunPrompt(promptSpecEmpty()); err == nil {
		t.Fatal("no terminal, no prompt")
	}
}

// A prompt refuses every non-terminal, and refuses it AT ONCE.
//
// ⚠ **The defect this exists for was not a wrong answer, it was a slow one.** RunPrompt gated on
// `os.ModeCharDevice`, which is set for /dev/null and for Windows' NUL — the two canonical ways of
// saying "there is nobody here" — so the gate opened on exactly the input that means no. What
// followed was not an error: bubbletea on Windows reaches the console directly rather than through
// stdin, so it entered the alternate screen and waited for a keypress that was never coming.
// Measured 2026-09-11: 6m46s, the whole package past its timeout, and raw escape sequences painted
// into the terminal of whoever ran it. In a headless run — a service, a CI step, `magi <
// /dev/null` — the same thing is a daemon that stops answering.
//
// So the deadline is the assertion. `err != nil` alone was already true on Linux, where bubbletea
// fails to raw-mode /dev/null and returns quickly, which is why this went nine months unseen: the
// old test passed on one platform for a reason that had nothing to do with the gate.
//
// Both ends, because a form drawn into a redirected stdout is escape sequences in somebody's
// output — the same defect pointed the other way.
func TestAPromptRefusesEveryNonTerminalPromptly(t *testing.T) {
	pipeR := func(t *testing.T) *os.File {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { r.Close(); w.Close() })
		return r
	}
	devnull := func(t *testing.T) *os.File {
		f, err := os.Open(os.DevNull)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		return f
	}
	for _, c := range []struct {
		name string
		open func(*testing.T) *os.File
		end  string // which end is not a terminal
	}{
		{"stdin is NUL/devnull", devnull, "in"},
		{"stdin is a pipe", pipeR, "in"},
		{"stdout is NUL/devnull", devnull, "out"},
		{"stdout is a pipe", pipeR, "out"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := c.open(t)
			oldIn, oldOut := os.Stdin, os.Stdout
			if c.end == "in" {
				os.Stdin = f
			} else {
				os.Stdout = f
			}
			defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()

			done := make(chan error, 1)
			go func() {
				_, err := RunPrompt(promptSpecEmpty())
				done <- err
			}()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("no terminal, no prompt")
				}
			case <-time.After(5 * time.Second):
				// Deliberately not waiting it out: the run that produced this bound held the
				// console for six minutes and forty-six seconds, and a test that waits for that
				// to end is the same defect wearing a timeout.
				t.Fatal("RunPrompt did not refuse — it is holding the terminal waiting for a " +
					"keypress that cannot come; the gate is asking ModeCharDevice again")
			}
		})
	}
}

// The terminal question is asked ONE way in this tree.
//
// `os.ModeCharDevice` reads like a terminal check and is not one, and it is the shape that drifts
// back: it needs no import, it is true on an ordinary tty, and it is wrong only for the inputs a
// person testing by hand does not have. term.IsTerminal was already the spelling in cmd/magi's
// drawsTUI and in all three width probes — five places against one — and the one that had drifted
// was the gate on the only surface that BLOCKS when it guesses wrong.
//
// Held as a source check because the behaviour test above cannot see a second copy appearing on a
// surface it does not call.
func TestTheTerminalQuestionIsAskedOneWay(t *testing.T) {
	// Parsed rather than grepped: prose about the rule is not a use of it, and the comment above
	// isInteractive spells `os.ModeCharDevice` out on purpose. A text scan flagged that comment,
	// which is a guard that punishes writing the reason down.
	var found []string
	for _, root := range []string{".", filepath.Join("..", "..", "..", "cmd", "magi")} {
		fset := token.NewFileSet()
		pkgs, err := parser.ParseDir(fset, root, func(fi os.FileInfo) bool {
			return !strings.HasSuffix(fi.Name(), "_test.go")
		}, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, pkg := range pkgs {
			for _, file := range pkg.Files {
				ast.Inspect(file, func(n ast.Node) bool {
					sel, ok := n.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "ModeCharDevice" {
						return true
					}
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == "os" {
						found = append(found, fset.Position(sel.Pos()).String())
					}
					return true
				})
			}
		}
	}
	if len(found) > 0 {
		t.Errorf("os.ModeCharDevice 로 단말을 묻는 자리가 남아 있다 — /dev/null 과 NUL 이 둘 다 문자 "+
			"장치라 「아무도 없다」가 「단말이다」로 답한다. term.IsTerminal 로 물어야 한다: %v", found)
	}
}
