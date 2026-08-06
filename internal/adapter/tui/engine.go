package tui

import (
	"context"

	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// Engine is everything the terminal UI asks of the thing running the agent.
//
// It exists to make that boundary a THING rather than a habit. Until now the UI held an *app.App
// by pointer, so "what does the screen need from the engine" was answerable only by grepping, and
// the answer changed silently whenever someone reached for one more method. It is 39 methods; that
// number is worth knowing before deciding what a second implementation would cost.
//
// Declared here, on the consuming side, on purpose. app does not know the UI exists and should not
// have to: adding a method to App cannot break this, and dropping one it declares fails the
// assertion below at compile time rather than at the call site.
//
// Nothing about this makes the engine remote. It makes a remote engine POSSIBLE — a client that
// satisfies this interface can stand where *app.App stands — but no transport is chosen here and
// no behaviour changes. That was the point of doing it first and by itself.
type Engine interface {
	BackgroundJobs() []app.BackgroundJob
	BackgroundTail(id string, max int) string
	Compact(ctx context.Context, c command.Compact) error
	ContextView(ctx context.Context, sid session.SessionID) (string, error)
	CouncilMemberNames() []string
	CreateSession(ctx context.Context, c command.CreateSession) (session.SessionID, error)
	Fork(ctx context.Context, sid session.SessionID, upToSeq int64) (session.SessionID, error)
	GitDiff(ctx context.Context, workdir string) (string, error)
	Interrupt(ctx context.Context, c command.Interrupt) error
	KillBackgroundJob(id string) bool
	ListModels(ctx context.Context) ([]string, error)
	ListSessions(ctx context.Context, workdir string) ([]session.SessionMeta, error)
	LoopMap(ctx context.Context, sid session.SessionID) (string, error)
	Permission() string
	Profiles() []app.ProfileDef
	Replay(ctx context.Context, sid session.SessionID) (session.SessionID, string, error)
	RespondPermission(ctx context.Context, c command.RespondPermission) error
	RespondQuestion(ctx context.Context, c command.RespondQuestion) error
	Rewind(ctx context.Context, sid session.SessionID, n int) (int64, error)
	RunShell(ctx context.Context, workdir, cmd string) (out string, exit int, err error)
	SessionDiff(ctx context.Context, aSid, bSid session.SessionID) (string, error)
	SessionModel(sid session.SessionID) string
	SessionState(ctx context.Context, sid session.SessionID) ([]session.Message, int64, error)
	SetContextWindow(ctx context.Context, sid session.SessionID, id string, tokens int) (string, error)
	SetGroupEnabled(group string, on bool) error
	SetModel(sid session.SessionID, modelID string)
	SetPermission(p string)
	SetProfile(p app.ProfileDef)
	SetSubagentEnabled(name string, on bool) error
	SetSubagentModel(name, model, provider string) error
	SetTodos(sid session.SessionID, td []session.Todo)
	Steer(ctx context.Context, c command.SubmitPrompt) error
	SubagentJobs() []app.SubagentJob
	Subagents() []app.SubagentInfo
	Submit(ctx context.Context, c command.SubmitPrompt) error
	Subscribe(ctx context.Context, sid session.SessionID, fromSeq int64) (<-chan event.Event, func(), error)
	Todos(sid session.SessionID) []session.Todo
	ToolNames() []string
	UnfinishedTurnOf(ctx context.Context, sid session.SessionID) (app.UnfinishedTurn, bool)
	UsageFor(sid session.SessionID) event.Usage
	UsageTotal() event.Usage
	UserLabel(sid session.SessionID) string
}

// The in-process engine. A method that drifts out of App breaks the build here, at the definition,
// instead of at whichever screen happened to call it.
var _ Engine = (*app.App)(nil)
