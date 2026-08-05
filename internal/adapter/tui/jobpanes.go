package tui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/session"
)

// The pane strip: a live sub-view per long-running thing, focusable, zoomable, interruptible,
// fading out a few seconds after it finished.
//
// It has two producers. A BACKGROUND COMMAND, which the agent starts and polls with bash_output
// while the terminal would otherwise show one line saying a process started and then nothing at
// all. And a SPAWNED CHILD, which runs for minutes inside a single tool call on a session id the
// TUI does not subscribe to — the parent's spinner and one clipped progress line were the only
// sign of it. Both are polled from a registry rather than pushed, and both fill the same pane,
// because the difference is what a pane SAYS and not how it is shown.
//
// So the panes carry those instead. The machinery is unchanged (focus, zoom, fade, click
// hit-testing); only what feeds them is different: a subscription becomes a poll, because a detached
// process writes to a file rather than to the event bus.
//
// Watching consumes nothing. The tail is read from the file's end and never advances the offset
// bash_output uses, so what the agent reads is exactly what it would have read unwatched.

// jobPollInterval is how often the strip re-reads the job registry and each job's tail. Fast enough
// that a build's output looks live, slow enough that a long log is not re-read sixteen times a
// second (the render tick's cadence).
const jobPollInterval = 700 * time.Millisecond

// jobTailBytes bounds one poll's read per job. The pane shows a tail, so more than this could never
// be displayed; the cap is what keeps an 8 MiB runaway log from being read on every poll.
const jobTailBytes = 8 << 10

type jobPollMsg struct{}

func jobPoll() tea.Cmd {
	return tea.Tick(jobPollInterval, func(time.Time) tea.Msg { return jobPollMsg{} })
}

// syncJobPanes reconciles the pane strip with the background-job registry: a new job opens a pane,
// a running one has its tail refreshed, and one that has exited is marked done so the existing fade
// carries it out of the strip and into the panel's record. Reports whether anything changed.
func (m *Model) syncJobPanes() bool {
	if m.app == nil {
		return false
	}
	changed := false
	for _, j := range m.app.BackgroundJobs() {
		p := m.paneByJob(j.ID)
		if p == nil {
			m.subID++
			p = &agentPane{
				job:     j.ID,
				role:    j.ID,
				task:    j.Command,
				started: j.Started,
				sub:     m.subID,
			}
			m.panes = append(m.panes, p)
			changed = true
		}
		if tail := m.app.BackgroundTail(j.ID, jobTailBytes); tail != p.live {
			p.live = tail
			changed = true
		}
		// A job that has exited (or been killed) is finished exactly once: doneAt starts the fade,
		// and re-marking it on every poll would restart that fade forever.
		if !j.Running && !p.done {
			p.done = true
			p.doneAt = time.Now()
			p.dur = time.Since(j.Started)
			p.exit, p.exited = j.Exit, true
			p.killed = j.Killed
			changed = true
		}
	}
	return changed || m.syncSubagentPanes(m.app.SubagentJobs())
}

// syncSubagentPanes gives the strip its second producer: a spawned child.
//
// A child runs for minutes inside one tool call, on its own session id that the TUI does not
// subscribe to — so before this the only sign of it was the parent's spinner and a single clipped
// line. It reuses the strip wholesale (the detail view, the side panel, the fade), because the
// difference between a background command and a child agent is what the pane SAYS, not how it is
// shown.
// Takes the list rather than reading it, so the mapping from a child to a pane can be exercised
// without standing up a run loop and an LLM to produce one.
func (m *Model) syncSubagentPanes(jobs []app.SubagentJob) bool {
	changed := false
	for _, j := range jobs {
		p := m.paneByJob(j.ID)
		if p == nil {
			m.subID++
			p = &agentPane{
				sid:     session.SessionID(j.ID),
				job:     j.ID,
				role:    j.Tool,
				task:    oneLine(j.Task, 200),
				started: j.Started,
				sub:     m.subID,
			}
			m.panes = append(m.panes, p)
			changed = true
		}
		if j.Tail != p.live {
			p.live = j.Tail
			changed = true
		}
		// Finished exactly once: doneAt starts the fade, and re-marking it every poll would
		// restart that fade forever — the same trap the job loop above documents.
		if !j.Running && !p.done {
			p.done = true
			p.doneAt = time.Now()
			p.dur = j.Ended.Sub(j.Started)
			// A child that stopped badly must not wear the same mark as one that finished: exit 0
			// is the pane's "it worked", and a truncated plan reported as success is exactly what
			// the caller cannot see for itself.
			p.exited = true
			if j.Err != "" {
				p.exit = 1
			}
			changed = true
		}
	}
	return changed
}

// paneByJob finds the pane showing background job id, or nil.
func (m *Model) paneByJob(id string) *agentPane {
	for _, p := range m.panes {
		if p.job == id {
			return p
		}
	}
	for _, p := range m.doneRoster {
		if p.job == id {
			return p
		}
	}
	return nil
}

// jobStatus is the pane's status line: how long it has been running, or how it ended. A killed job
// says so rather than reporting an exit code — the agent stopped it, so the code says nothing about
// the work.
func jobStatus(p *agentPane) string {
	switch {
	case p.killed:
		return "killed"
	case p.exited && p.exit == 0:
		return fmt.Sprintf("exited 0 · %s", p.dur.Round(time.Second))
	case p.exited:
		return fmt.Sprintf("exited %d · %s", p.exit, p.dur.Round(time.Second))
	default:
		return "running " + time.Since(p.started).Round(time.Second).String()
	}
}
