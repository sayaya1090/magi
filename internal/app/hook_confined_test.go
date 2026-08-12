package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// A hook is confined like the tool call that triggered it.
//
// It ran unconfined while `bash` two files away was wrapped, so the strictest posture on the
// machine still had one path to a plain /bin/sh — and it was the path that fires on EVERY tool
// use, from a command line a committed config file supplied.
func TestAHookRunsUnderTheSameConfinementAsBash(t *testing.T) {
	var ran port.Cmd
	plat := &scriptPlatform{onExec: func(c port.Cmd) { ran = c }}
	a := closeAfter(t, New(nil, nil, nil, nil, plat, Config{
		Sandbox:  "workspace-write",
		StoreDir: t.TempDir(), // the transcripts, which no confined child may write
		Hooks:    []HookSpec{{Event: "PreToolUse", Command: "echo hi"}},
		Confine: func(spec port.SandboxSpec, argv []string) ([]string, bool) {
			if spec.Mode != "workspace-write" {
				t.Errorf("the hook was offered mode %q", spec.Mode)
			}
			if len(spec.ReadOnly) == 0 {
				t.Error("the hook's confinement does not keep the transcript read-only")
			}
			return append([]string{"/usr/bin/jail"}, argv...), true
		},
	}))
	a.runPreToolHooks(context.Background(), t.TempDir(), "bash", "")

	if ran.Path != "/usr/bin/jail" {
		t.Fatalf("the hook was started as %q %v — outside the sandbox", ran.Path, ran.Args)
	}
	if strings.Join(ran.Args, " ") != "/bin/sh -c echo hi" {
		t.Errorf("the command inside the sandbox is %v", ran.Args)
	}
}

// And with nothing to confine with, it runs as it always did rather than not at all.
func TestAHookStillRunsWhereThereIsNoSandbox(t *testing.T) {
	var ran port.Cmd
	plat := &scriptPlatform{onExec: func(c port.Cmd) { ran = c }}
	a := closeAfter(t, New(nil, nil, nil, nil, plat, Config{
		Hooks: []HookSpec{{Event: "PreToolUse", Command: "echo hi"}},
	}))
	a.runPreToolHooks(context.Background(), t.TempDir(), "bash", "")
	if ran.Path != "/bin/sh" {
		t.Errorf("the hook ran as %q", ran.Path)
	}
}
