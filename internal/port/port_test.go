package port

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

func TestSandboxConfined(t *testing.T) {
	if (SandboxSpec{}).Confined() {
		t.Error("zero-value spec must not be confined")
	}
	if !(SandboxSpec{Mode: "read-only"}).Confined() || !(SandboxSpec{Mode: "workspace-write"}).Confined() {
		t.Error("read-only and workspace-write must be confined")
	}
	if (SandboxSpec{Mode: "full"}).Confined() {
		t.Error("full mode is unconfined")
	}
}

// A tool says what it is by implementing MetaTool, and one that says nothing means nothing.
//
// This is the read side of the optional-interface pattern the whole subagent list rests on: whether
// a tool is a subagent, which group it is in, and whether it ships off are all decided here. The
// fallback matters as much as the hit — every built-in goes down it, and a zero value that came
// back with Subagent set would put every built-in tool in /subagents.
func TestToolMetadataIsWhatTheToolDeclaresAndNothingOtherwise(t *testing.T) {
	declared := ToolMetadata{Subagent: true, Group: "planning", DefaultOff: true, Internal: true}
	if got := ToolMetaOf(metaStub{meta: declared}); got != declared {
		t.Errorf("a tool's own metadata came back as %+v, want %+v", got, declared)
	}
	// A plain Tool declares none, and none must read as the zero value rather than as anything set.
	if got := ToolMetaOf(plainStub{}); got != (ToolMetadata{}) {
		t.Errorf("a tool that declares nothing came back as %+v", got)
	}
	// Specifically: it is not a subagent and not internal, the two flags that change where a tool
	// is offered.
	got := ToolMetaOf(plainStub{})
	if got.Subagent {
		t.Error("a tool that declares nothing would be listed in /subagents")
	}
	if got.Internal {
		t.Error("a tool that declares nothing would be withheld from the main agent")
	}
}

type plainStub struct{}

func (plainStub) Name() string            { return "plain" }
func (plainStub) Description() string     { return "d" }
func (plainStub) Schema() json.RawMessage { return nil }
func (plainStub) Execute(context.Context, json.RawMessage, ToolEnv) (session.ToolResult, error) {
	return session.ToolResult{}, nil
}

type metaStub struct {
	plainStub
	meta ToolMetadata
}

func (m metaStub) Meta() ToolMetadata { return m.meta }
