package builtin

import (
	"strings"
	"testing"
)

// The note fires at the moment the model is already blocked on a prompt it cannot answer, which
// is the worst possible moment to point it at a capability this platform refuses: pty:true is
// rejected outright where ptySupported is false (bgproc.go), so following the steer costs another
// turn and lands it back here. Both branches must still carry the obligation — do not sit on the
// prompt — and only the route may differ.
func TestPtyNeededNoteOffersOnlyWhatThisPlatformHas(t *testing.T) {
	note := ptyNeededNote("ssh alpine@localhost", false)
	if note == "" {
		t.Fatal("the note must fire for a tty-gated command without a pty")
	}
	if !strings.Contains(note, "controlling terminal") {
		t.Errorf("the note lost its explanation:\n%s", note)
	}
	if ptySupported {
		if !strings.Contains(note, "pty:true") || !strings.Contains(note, "bash_input") {
			t.Errorf("where a pty exists the steer is the pty path:\n%s", note)
		}
		return
	}
	if strings.Contains(note, "pty:true") {
		t.Errorf("this platform rejects pty:true — the note must not ask for it:\n%s", note)
	}
	if !strings.Contains(note, "BatchMode") && !strings.Contains(note, "sshpass") {
		t.Errorf("dropping the pty route must leave a route that works here:\n%s", note)
	}
}

// guardConsole is called around every foreground command, so it has to be safe with no console
// at all (tests, headless, redirected handles): a usable restorer, and calling it changes nothing.
func TestGuardConsoleIsSafeWithoutAConsole(t *testing.T) {
	restore := guardConsole()
	if restore == nil {
		t.Fatal("guardConsole must always return a callable restorer")
	}
	restore()
	guardConsole()() // and it is re-entrant across calls
}
