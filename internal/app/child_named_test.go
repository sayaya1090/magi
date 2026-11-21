package app

import (
	"errors"
	"reflect"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// The line format is a contract with every screen: ChildrenNamed must read exactly what the core
// writes, for a spawned subagent and for a child a tool drives.
func TestTheChildLinesTheCoreWritesAreReadBack(t *testing.T) {
	spawned := childAccount(port.SpawnResult{SessionID: "s_0123abcd", Steps: 3}, nil)
	failed := childAccount(port.SpawnResult{SessionID: "s_99ff"}, errors.New("no backend"))
	none := childAccount(port.SpawnResult{}, errors.New("refused"))
	driven := openChildAccount("s_feed01", 2, "")
	stopped := openChildAccount("s_feed02", 0, "the script stopped: boom")
	text := "what the tool said\n\n" + spawned + "\n" + failed + "\n" + none + "\n" + driven + "\n" + stopped
	got := ChildrenNamed(text)
	want := []string{"s_0123abcd", "s_99ff", "s_feed01", "s_feed02"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("read %v from\n%s\nwant %v", got, text, want)
	}
	if got := ChildrenNamed("a tool that only mentions [child s_abc, 1 step(s)] mid-line"); len(got) != 0 {
		t.Fatalf("only a line the core wrote names a child: %v", got)
	}
	if got := ChildrenNamed(spawned + "\n" + spawned); !reflect.DeepEqual(got, []string{"s_0123abcd"}) {
		t.Fatalf("a child named twice is one child: %v", got)
	}
}
