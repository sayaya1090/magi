package main

import "testing"

// A daemon must not ask a terminal for its background colour.
//
// "auto" writes a query to stdout and reads the reply from stdin. That is a conversation with a
// person's terminal emulator, and a daemon started by a launcher — a service manager, an IDE
// plugin, the PowerPoint helper — has a console handle nobody is driving. The query goes out, the
// answer never comes, and the process sits there BEFORE it has bound its socket.
//
// Measured on Windows (2026-09-02): with the default -theme auto, `magi --daemon` took 165s in one
// run and 659s in another to create its socket, at 0.1s of CPU. With -theme dark, or with stdin at
// NUL, the same start bound in 1s. From outside it looked like a daemon that never came up.
func TestADaemonNeverAsksTheTerminalForItsTheme(t *testing.T) {
	asked := false
	got := resolveTheme("auto", true, func() bool { asked = true; return false })
	if asked {
		t.Fatal("a daemon asked the terminal for its background — that is the wait that never ends")
	}
	if !got {
		t.Fatal("a daemon that cannot ask should keep the default, not the zero value of a failed ask")
	}
}

// An explicit theme is an instruction, not a detection — it is honoured in either mode, and it also
// never asks.
func TestAnExplicitThemeIsHonouredAndNeverAsks(t *testing.T) {
	for _, c := range []struct {
		theme  string
		daemon bool
		want   bool
	}{
		{"dark", false, true},
		{"dark", true, true},
		{"light", false, false},
		{"light", true, false},
	} {
		asked := false
		got := resolveTheme(c.theme, c.daemon, func() bool { asked = true; return !c.want })
		if asked {
			t.Fatalf("-theme %s asked the terminal anyway", c.theme)
		}
		if got != c.want {
			t.Fatalf("-theme %s (daemon=%v) gave %v", c.theme, c.daemon, got)
		}
	}
}

// With a terminal in front of a person, detection is still what decides. Removing the ask would fix
// the daemon by making every interactive run wrong, and this is the assertion that says so.
func TestAnInteractiveRunStillDetects(t *testing.T) {
	for _, want := range []bool{true, false} {
		asked := false
		got := resolveTheme("auto", false, func() bool { asked = true; return want })
		if !asked {
			t.Fatal("an interactive run stopped detecting the terminal background")
		}
		if got != want {
			t.Fatalf("detection said %v but the answer was %v", want, got)
		}
	}
}

// An unknown value falls back to detection rather than being rejected here — the flag's own
// validation is what refuses it, and this function must not add a second, different opinion.
func TestAnUnknownThemeTakesTheAutoPath(t *testing.T) {
	asked := false
	resolveTheme("chartreuse", false, func() bool { asked = true; return true })
	if !asked {
		t.Fatal("an unknown theme quietly stopped detecting instead of behaving like auto")
	}
}
