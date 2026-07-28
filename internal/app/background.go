package app

import (
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
)

// A background command is the one thing magi runs that the person watching could not see. The agent
// starts it, polls it with bash_output, and acts on what it reads; the terminal showed a line saying
// a process had started and then nothing — not its output, not when it ended, not whether it ended
// badly. These expose the registry so a viewer can follow it.
//
// Reading here consumes nothing. bash_output advances the agent's offset; a viewer that shared it
// would take output the agent has not seen, which is precisely the kind of effect the agent could
// not account for. The tail is read from the file's end and touches no offset.

// BackgroundJob is one background command, for display.
type BackgroundJob = builtin.BackgroundJob

// BackgroundJobs returns the background commands this process holds, oldest first. Not scoped to a
// session: the registry tracks OS processes, which outlive any one session by design.
func (a *App) BackgroundJobs() []BackgroundJob { return builtin.ListBackgroundJobs() }

// BackgroundTail returns the last max bytes of a job's output for display, without advancing what
// the agent has read.
func (a *App) BackgroundTail(id string, max int) string { return builtin.TailBackgroundJob(id, max) }

// KillBackgroundJob stops a background command — the same stop bash_kill performs, offered to the
// person watching it. Reports whether a job with that id existed.
func (a *App) KillBackgroundJob(id string) bool { return builtin.KillBackgroundJob(id) }
