package app

import (
	"context"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// What magi says to the agent mid-turn, on its own initiative.
//
// These are the surviving injections after the planning stages came out. Each carries something
// the agent could not otherwise have: a council's reading of its work, or a steer the user typed
// while the turn was running. Neither is a contract and neither is hidden — they arrive as
// ordinary messages in the conversation, attributed to who wrote them, which is the whole
// difference between telling the agent something and doing something behind it.

// injectCouncilAdvice hands the council's notes to the agent as a system message. The council
// advises; what to do about the notes is the agent's call, so the text says so rather than
// ordering a revision.
func (a *App) injectCouncilAdvice(ctx context.Context, sid session.SessionID, advice string, approved bool) {
	tail := "The council raised the notes above. Incorporate them where they improve the result — " +
		"they are not blocking, so carry on with the task."
	if !approved {
		tail = "The council could not resolve the concerns above within its round cap, and is not " +
			"holding the turn on them. Address them as you continue."
	}
	text := "# Council review — notes on the work\n\n" + advice + "\n\n---\n" + tail
	_ = a.appendPromptText(ctx, sid, event.Actor{Kind: event.ActorSystem, ID: "council"}, text)
}

// injectSteerConstraint folds a mid-turn "append" steer into the RUNNING turn as a constraint.
// The steer adjusts HOW the in-progress work is carried out, not what the task is; the loop keeps
// turnTask = original+steer, so the finish gate judges against both.
func (a *App) injectSteerConstraint(ctx context.Context, sid session.SessionID, steer string) {
	text := "# Mid-task steer (from the user)\n\n" + steer + "\n\n---\n" +
		"Apply this as a constraint on the work already in progress — do NOT restart. Adjust how you " +
		"carry out what remains so this holds, and make sure it does before you finish."
	_ = a.appendPromptText(ctx, sid, event.Actor{Kind: event.ActorSystem, ID: "steer"}, text)
}
