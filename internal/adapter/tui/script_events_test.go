package tui

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/artifact"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// Sweep twelve: the events nothing had ever fed the view.
//
// Eight of the twenty-six event types had never appeared in a test. Each is something magi says
// about itself — who it is talking to, which model it switched to, what it produced, what the user
// decided — and a fold that drops one is magi keeping a fact to itself.

// A user label arrives from an SSO plugin and every bubble should adopt it. Nobody wants to see
// "you" after magi has been told their name — and worse, a label that lands mid-session must not
// leave earlier bubbles addressed to someone else.
func TestAUserLabelAppliesToTheWholeTranscript(t *testing.T) {
	s := newScript(t)
	s.steer("r1", "before the label")
	s.assistantText("a reply")
	s.emitAs(event.TypeUserLabelChanged, event.Actor{Kind: event.ActorSystem, ID: "plugin"},
		event.UserLabelData{Label: "sangjay"})
	s.steer("r2", "after the label")

	plain := s.view()
	if !strings.Contains(plain, "sangjay") {
		t.Errorf("the label magi was given is not on screen:\n%s", plain)
	}
	if strings.Count(plain, " you ") > 0 {
		t.Errorf("some bubbles still say \"you\" after a label was set:\n%s", plain)
	}
}

// Switching model mid-session. The header names the model every request goes to, so a stale one is
// magi telling the user their work is going somewhere it is not.
func TestTheHeaderFollowsAModelSwitch(t *testing.T) {
	s := newScript(t)
	s.steer("r1", "go")
	s.emitAs(event.TypeModelChanged, event.Actor{Kind: event.ActorSystem, ID: "route"},
		event.ModelChangedData{Model: "qwen3-coder-next:latest"})
	s.renders("after a model switch", "qwen3-coder-next")
}

// An artifact is a file magi produced and wants the user to find. Emitting one and showing nothing
// is the same as not producing it.
func TestAnArtifactIsVisible(t *testing.T) {
	s := newScript(t)
	s.steer("r1", "make the report")
	s.emit(event.TypeArtifactEmitted, event.ArtifactEmittedData{
		Artifact: artifact.Artifact{ID: "a1", Kind: "file", Title: "report.md", SourceAgent: "default"},
	})
	plain := s.view()
	if !strings.Contains(plain, "report.md") {
		t.Errorf("an artifact magi produced is nowhere on screen:\n%s", plain)
	}
}

// A cancelled prompt. The user will never get an answer to it, and a bubble that goes on looking
// like it is waiting is the display promising something nothing will deliver.
func TestAnAbandonedPromptStopsLookingLikeItIsWaiting(t *testing.T) {
	s := newScript(t)
	s.steer("r1", "the first task")
	s.steer("r2", "the cancelled one")
	s.emitAs(event.TypePromptAbandoned, event.Actor{Kind: event.ActorSystem, ID: "loop"},
		event.PromptAbandonedData{MsgID: "r2"})

	for _, b := range s.m.blocks {
		if b.reqID == "r2" && b.queued && !b.abandoned {
			t.Error("a cancelled request still shows as waiting for a turn that will never come")
		}
	}
	s.renders("after a prompt was abandoned", "the cancelled one")
}

// The council names the member it is polling right now. It is the difference between "thinking"
// and "waiting on Balthasar", and it is the only progress signal a deliberation gives.
func TestTheCouncilNamesWhoItIsPolling(t *testing.T) {
	s := newScript(t)
	s.steer("r1", "finish")
	s.emit(event.TypeCouncilConvened, event.CouncilConvenedData{
		Round: 1, Members: []string{"Melchior", "Balthasar", "Casper"}, Rule: "majority", Task: "t"})
	s.emit(event.TypeCouncilDeliberating, event.CouncilDeliberatingData{
		Round: 1, Member: "Balthasar", State: "asking"})
	if s.m.councilMember != "Balthasar" {
		t.Errorf("the polled member is not recorded, got %q", s.m.councilMember)
	}
	s.renders("while a member is being polled", "Balthasar")
}

// A permission decision is an audit fact: the user allowed or denied this call. It is recorded
// precisely so it can be read back, and a resumed session that cannot say which way it went has
// lost the one thing the prompt existed to capture.
func TestAPermissionDecisionSurvivesInTheRecord(t *testing.T) {
	s := newScript(t)
	s.steer("r1", "delete them")
	s.toolCall("bash", "c1")
	s.emit(event.TypePermissionDecided, event.PermissionDecidedData{CallID: "c1", Decision: "deny"})
	s.toolResult("c1", "denied by the user")
	s.renders("after a denied call", "denied by the user")
}

// tool.started is the moment a call leaves for the shell. The view may or may not draw it, but it
// must not corrupt the transcript — a fold that mishandles an unknown-to-it event is how a session
// ends up one block short of the truth.
func TestToolStartedDoesNotDisturbTheTranscript(t *testing.T) {
	s := newScript(t)
	s.steer("r1", "run it")
	s.toolCall("bash", "c1")
	before := blockSummary(s.m.blocks)
	s.emit(event.TypeToolStarted, event.ToolStartedData{CallID: "c1", Name: "bash"})
	after := blockSummary(s.m.blocks)
	if len(before) != len(after) {
		t.Fatalf("tool.started changed the block count: %d → %d", len(before), len(after))
	}
	s.toolResult("c1", "ok")
	s.renders("after the call completed", "ok")
	_ = session.RoleAssistant
}
