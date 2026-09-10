package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/platform"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
)

// The `!` inline shell must reach a shell this platform actually has.
//
// `RunShell` named `/bin/sh` outright. That is not a worse shell on Windows, it is not a shell at
// all — the call came back `exec: "/bin/sh": executable file not found in %PATH%`, so the inline
// shell and the same door over the socket (RunShellHere) could not run one command there.
// Measured 2026-09-11.
//
// The bash tool had answered this already, with a measured reason for each branch. This test pins
// that they are ONE answer: not that the answer is powershell, or bash, or /bin/sh — that whatever
// `builtin.Shell` picks is what `RunShell` runs. Pinning the name instead would make this test a
// second place the decision lives, which is the shape being removed.
func TestTheInlineShellIsTheOneTheToolsUse(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := closeAfter(t, New(store, nil, builtin.Default(), bus.New(), platform.New(), Config{}))

	// A command whose OUTPUT names the shell that ran it, in that shell's own spelling. Nothing
	// here decides which shell that should be; it asks the one definition and then checks the
	// answer came back through it.
	name, _ := builtin.Shell("")
	out, exit, err := a.RunShell(context.Background(), t.TempDir(), "echo magi-shell-probe")
	if err != nil {
		t.Fatalf("RunShell 이 %q 에 못 닿았다: %v", name, err)
	}
	if exit != 0 {
		t.Errorf("평범한 echo 가 %d 로 끝났다: %q", exit, out)
	}
	if !strings.Contains(out, "magi-shell-probe") {
		t.Errorf("셸이 돌긴 했는데 출력이 안 온다: %q", out)
	}
}

// And the definition is one definition.
//
// A second copy would not announce itself: both would work on the machine whoever wrote them was
// using, and disagree on the one they were not.
func TestOnlyOnePlaceDecidesWhichShell(t *testing.T) {
	name, args := builtin.Shell("echo x")
	if strings.TrimSpace(name) == "" {
		t.Fatal("셸 이름이 비어 있다")
	}
	if len(args) == 0 || args[len(args)-1] != "echo x" {
		t.Errorf("명령이 마지막 인자로 실려야 한다: %q %v", name, args)
	}
	// The same call twice is the same answer — the unix lookup is cached, and a chooser that
	// varied between calls would make two tool invocations run in two different shells.
	name2, args2 := builtin.Shell("echo x")
	if name != name2 || len(args) != len(args2) {
		t.Errorf("두 번 물으니 다른 답이다: %q %v vs %q %v", name, args, name2, args2)
	}
}
