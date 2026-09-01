package main

import (
	"os"
	"strings"
	"testing"
)

// Nothing that will not draw a TUI asks the terminal for its background colour.
//
// "auto" writes a query to stdout and reads the reply from stdin. That is a conversation with a
// person's terminal emulator. A process a launcher started — a service manager, an IDE plugin, the
// PowerPoint helper — has no such partner, and on Windows it does not simply fail: lipgloss sees
// redirected stdio and opens CONIN$/CONOUT$ explicitly, putting the query on a console handle
// nobody is driving. The query goes out and the answer never comes.
//
// Measured on Windows (2026-09-02): with the default -theme auto, `magi --daemon` took 165s in one
// run and 659s in another to create its socket, at 0.1s of CPU. With -theme dark, or with stdin at
// NUL, the same start bound in 1s. From outside it looked like a daemon that never came up.
//
// The first fix keyed on --daemon alone. That was too narrow, and this table is the reason: every
// row below is a real command a launcher runs, and every one of them used to ask.
func TestNothingHeadlessAsksTheTerminalForItsTheme(t *testing.T) {
	for _, what := range []string{
		"a daemon",
		"a headless -p run",
		"a command whose stdout was redirected to a file",
		"a command a launcher started with no console of its own",
	} {
		asked := false
		got := resolveTheme("auto", false, func() bool { asked = true; return false })
		if asked {
			t.Fatalf("%s asked the terminal for its background — that is the wait that never ends", what)
		}
		if !got {
			t.Fatalf("%s should keep the default, not the zero value of an ask it never made", what)
		}
	}
}

// An explicit theme is an instruction, not a detection — honoured either way, and it never asks.
func TestAnExplicitThemeIsHonouredAndNeverAsks(t *testing.T) {
	for _, c := range []struct {
		theme string
		draws bool
		want  bool
	}{
		{"dark", true, true},
		{"dark", false, true},
		{"light", true, false},
		{"light", false, false},
	} {
		asked := false
		got := resolveTheme(c.theme, c.draws, func() bool { asked = true; return !c.want })
		if asked {
			t.Fatalf("-theme %s asked the terminal anyway", c.theme)
		}
		if got != c.want {
			t.Fatalf("-theme %s (draws=%v) gave %v", c.theme, c.draws, got)
		}
	}
}

// With a person in front of a real terminal, detection is still what decides. Removing the ask
// would fix the launcher by making every interactive run wrong, and this says so.
func TestAnInteractiveRunStillDetects(t *testing.T) {
	for _, want := range []bool{true, false} {
		asked := false
		got := resolveTheme("auto", true, func() bool { asked = true; return want })
		if !asked {
			t.Fatal("an interactive run stopped detecting the terminal background")
		}
		if got != want {
			t.Fatalf("detection said %v but the answer was %v", want, got)
		}
	}
}

// An unknown value takes the auto path rather than being rejected here — the flag's own validation
// refuses it, and this function must not add a second, different opinion.
func TestAnUnknownThemeTakesTheAutoPath(t *testing.T) {
	asked := false
	resolveTheme("chartreuse", true, func() bool { asked = true; return true })
	if !asked {
		t.Fatal("an unknown theme quietly stopped detecting instead of behaving like auto")
	}
}

// The predicate must be built from the mode, and the mode is not known where the old call sat.
//
// This reads the source because the ordering IS the defect: `headless` is computed part-way down
// main(), and the theme used to be resolved above it — so the call could not have consulted the
// right thing even if it wanted to. `--version` sat in between, which is why a launcher's health
// check hung too. A future edit that moves the resolution back up would compile and pass every
// assertion above.
func TestTheThemeIsResolvedAfterTheModeAndAfterVersion(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	at := func(needle string) int {
		i := strings.Index(string(src), needle)
		if i < 0 {
			t.Fatalf("could not find %q in main.go", needle)
		}
		return i
	}
	version := at("\tif *showVersion {")
	headless := at("\theadless := pSet || *daemonMode")
	theme := at("\tisDark := resolveTheme(")

	if theme < headless {
		t.Fatal("the theme is resolved before the mode is known — the predicate cannot be right there")
	}
	if theme < version {
		t.Fatal("`magi --version` resolves the theme before it prints; a launcher's health check hangs")
	}
	if !strings.Contains(string(src), "term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd())") {
		t.Fatal("the predicate no longer requires a real terminal on BOTH ends — " +
			"on Windows lipgloss opens CONIN$/CONOUT$ when stdio is redirected, and that is the hang")
	}
}
