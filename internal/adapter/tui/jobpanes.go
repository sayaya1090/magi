package tui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
)

// The pane strip used to show subagents: a live sub-view per spawned child, focusable, zoomable,
// interruptible, fading out a few seconds after it finished. Nothing spawns any more, and the one
// thing magi still runs that nobody could watch is a background command — the agent starts it, polls
// it with bash_output, and acts on what it reads, while the terminal shows a single line saying a
// process started and then nothing at all.
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
