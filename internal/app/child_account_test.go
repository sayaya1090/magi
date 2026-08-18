package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// The parent's log is the parent's context, and until now nothing about a child reached it:
// spawnChild writes only to the child's session, and the progress it emits to the parent is
// transient. Whether the child's answer arrived at all was left to whatever the plugin returned.
// So the account has to carry the things a summary drops first — the id that opens the child's own
// transcript, and the failure it stopped on.
func TestAChildsAccountCarriesWhatASummaryWouldDrop(t *testing.T) {
	got := childAccount(port.SpawnResult{
		SessionID: "s_child_42", Steps: 7,
		Text: "the flake is in TestRetry: it asserts on wall-clock, not the fake clock",
	}, nil)

	for _, want := range []string{"s_child_42", "7 step", "TestRetry", "fake clock"} {
		if !strings.Contains(got, want) {
			t.Errorf("the account dropped %q:\n%s", want, got)
		}
	}
}

// A child that stopped short and is reported as silence reads as success.
func TestAChildThatStoppedShortSaysSo(t *testing.T) {
	short := childAccount(port.SpawnResult{SessionID: "s_1", Steps: 3, Err: "step budget spent"}, nil)
	if !strings.Contains(short, "stopped short") || !strings.Contains(short, "step budget spent") {
		t.Errorf("a child that ran out is reported as if it finished:\n%s", short)
	}
	never := childAccount(port.SpawnResult{}, errors.New("clone failed"))
	if !strings.Contains(never, "did not run") || !strings.Contains(never, "clone failed") {
		t.Errorf("a child that never ran is reported as if it did:\n%s", never)
	}
	quiet := childAccount(port.SpawnResult{SessionID: "s_2", Steps: 1}, nil)
	if !strings.Contains(quiet, "without saying anything") {
		t.Errorf("a silent child leaves the account claiming nothing at all:\n%s", quiet)
	}
}

// The cap protects the window the account is written into — but a cut the reader cannot see is a
// claim that the account was whole.
func TestTheCutIsAnnounced(t *testing.T) {
	got := childAccount(port.SpawnResult{
		SessionID: "s_3", Steps: 2, Text: strings.Repeat("x", childAccountCap+500),
	}, nil)
	if len(got) > childAccountCap+400 {
		t.Errorf("the account is not bounded: %d bytes", len(got))
	}
	if !strings.Contains(got, "cut here") || !strings.Contains(got, "own transcript") {
		t.Errorf("the cut is silent, so the account reads as complete:\n%s", got[len(got)-200:])
	}
}
