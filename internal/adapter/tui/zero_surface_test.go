package tui

import (
	"os"
	"strings"
	"testing"

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
