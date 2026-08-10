package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/config"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/cron"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// The work a workspace does when nobody is watching.
//
// # Only a daemon fires these
//
// RunCron is called from one place: the daemon branch in cmd/magi. An interactive session reads the
// same jobs so its editor can show them and fires none of them. This is not a policy that can be
// relaxed later without thinking — three terminals open in one repo would be three companions all
// running the nightly audit, against the same files, at the same second.
//
// # A fire is a new session
//
// Not a steer into whatever the daemon is already doing. A scheduled prompt arriving mid-turn would
// change what a person asked for, and a job's output would be buried in a conversation about
// something else. Its own session means it shows up in the resume list and the console history like
// any other work, and the only new thing needed to recognise it later is who asked: the actor id is
// "cron:<name>".
//
// # What it does not do
//
// It does not catch up. A daemon started at noon does not run the three-o'clock job on the way past,
// and a laptop that slept through four hours of an hourly job wakes to one fire and not four. The
// alternative is a machine coming back from a weekend and immediately running everything it missed,
// which is how a scheduler turns into an incident.
//
// It also keeps no record of what it ran. The sessions it created ARE the record — see
// SessionMeta.Origin — because a second log of when something happened is a second log that can
// disagree with the first.

// cronActor is who a scheduled prompt comes from.
//
// The kind is ActorUser and not ActorSystem, which looks wrong for something no person typed and is
// not: only an ActorUser prompt starts a top-level turn (see council_evidence.go). A system-kind
// prompt would land in the log and be skipped by the very machinery meant to answer it, so the job
// would create a session, write one message into it and sit there. The id carries the truth that
// the kind cannot.
func cronActor(name string) event.Actor {
	return event.Actor{Kind: event.ActorUser, ID: cronOriginPrefix + name}
}

// cronOriginPrefix marks an actor id as a scheduled job's. Also read by the session store to fill
// SessionMeta.Origin, which is how the editors show a job's last run without a second ledger.
const cronOriginPrefix = "cron:"

// CronOriginName returns the job name in an actor id written by cronActor, and whether it was one.
func CronOriginName(actorID string) (string, bool) {
	return strings.CutPrefix(actorID, cronOriginPrefix)
}

// scheduledJob is one job, parsed and ready, with the instant it is next owed.
type scheduledJob struct {
	Name     string
	Schedule cron.Schedule
	Prompt   string
	// Due is the next fire. Zero means never again — a schedule like "the 30th of February", which
	// parses cleanly and matches no instant.
	Due time.Time
}

type cronScheduler struct {
	app     *App
	workdir string
	now     func() time.Time
	report  func(string)
	jobs    []scheduledJob
	// last is the session each job most recently started, and the whole of the overlap check. There
	// is no separate "running" flag to keep in step with reality: whether that session is still
	// going is a question the App can already answer.
	last map[string]session.SessionID
}

// newCronScheduler parses the jobs and arms each one.
//
// A job that cannot be used is reported and dropped, never silently kept as a thing that will never
// fire: an unparseable schedule and a schedule of "never" look identical from the outside, and the
// person who mistyped one needs to hear about it at startup rather than wonder in a week.
func newCronScheduler(a *App, workdir string, jobs map[string]config.CronJob,
	now func() time.Time, report func(string)) *cronScheduler {

	s := &cronScheduler{app: a, workdir: workdir, now: now, report: report,
		last: map[string]session.SessionID{}}
	if report == nil {
		s.report = func(string) {}
	}
	// Sorted, so startup messages and the order of firing within one minute are the same on every
	// run. Map order would make two identical daemons disagree about which job went first.
	names := make([]string, 0, len(jobs))
	for name := range jobs {
		names = append(names, name)
	}
	sort.Strings(names)

	at := s.now()
	for _, name := range names {
		j := jobs[name]
		if !j.On() {
			continue
		}
		if strings.TrimSpace(j.Prompt) == "" {
			s.report(fmt.Sprintf("cron %q has no prompt — nothing to ask, so it is not scheduled", name))
			continue
		}
		sch, err := cron.Parse(j.Schedule)
		if err != nil {
			s.report(fmt.Sprintf("cron %q: %v — not scheduled", name, err))
			continue
		}
		due := sch.Next(at)
		if due.IsZero() {
			s.report(fmt.Sprintf("cron %q: %q never comes round — not scheduled", name, j.Schedule))
			continue
		}
		s.jobs = append(s.jobs, scheduledJob{Name: name, Schedule: sch, Prompt: j.Prompt, Due: due})
	}
	return s
}

// Jobs returns what is armed, in name order. For the daemon's startup line and for tests.
func (s *cronScheduler) Jobs() []scheduledJob { return s.jobs }

// tickOnce fires everything now owed. Separated from the waiting so a test can drive it with a
// clock it controls instead of waiting for real minutes to pass.
func (s *cronScheduler) tickOnce(ctx context.Context) {
	at := s.now()
	for i := range s.jobs {
		j := &s.jobs[i]
		if j.Due.IsZero() || at.Before(j.Due) {
			continue
		}
		// Re-arm from NOW, before firing and whatever the fire does.
		//
		// From now rather than from the missed time, so a machine that slept through four hourly
		// slots fires once and is then due in an hour — not four times in four seconds. Before
		// firing, so a job whose session cannot even be created is due again at its next slot
		// instead of being retried on every tick forever.
		j.Due = j.Schedule.Next(at)

		if prev, ok := s.last[j.Name]; ok && s.app.sessionRunning(prev) {
			s.report(fmt.Sprintf("cron %q: skipped, its last run (%s) is still going", j.Name, prev))
			continue
		}
		sid, err := s.fire(ctx, *j)
		if err != nil {
			s.report(fmt.Sprintf("cron %q: %v", j.Name, err))
			continue
		}
		s.last[j.Name] = sid
		s.report(fmt.Sprintf("cron %q: started session %s", j.Name, sid))
	}
}

// fire opens a session for one run of a job and asks it the job's prompt, verbatim.
func (s *cronScheduler) fire(ctx context.Context, j scheduledJob) (session.SessionID, error) {
	actor := cronActor(j.Name)
	// Model left zero on purpose: CreateSession fills in whatever this companion is configured to
	// run, so a job follows the workspace's model rather than pinning one that will go stale.
	sid, err := s.app.CreateSession(ctx, command.CreateSession{
		Workdir: s.workdir,
		Actor:   actor,
	})
	if err != nil {
		return "", fmt.Errorf("could not open a session: %w", err)
	}
	err = s.app.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: j.Prompt}},
		Actor:     actor,
	})
	if err != nil {
		return sid, fmt.Errorf("session %s opened but the prompt did not land: %w", sid, err)
	}
	return sid, nil
}

// sessionRunning reports whether a turn is in flight for sid.
//
// This is the overlap check, and it reads the App's own state rather than a flag the scheduler
// keeps: startRun sets cancel before it returns and the teardown clears it, so the answer is
// already maintained by the thing that knows. A second copy would be a second thing to keep true.
func (a *App) sessionRunning(sid session.SessionID) bool {
	if sid == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	st, ok := a.stateIf(sid)
	return ok && st.cancel != nil
}

// RunCron fires this workspace's scheduled jobs until ctx is done. It blocks; the daemon runs it in
// a goroutine. Returns immediately when nothing is armed, so a workspace with no jobs pays nothing.
func (a *App) RunCron(ctx context.Context, workdir string, jobs map[string]config.CronJob, report func(string)) {
	s := newCronScheduler(a, workdir, jobs, time.Now, report)
	if len(s.jobs) == 0 {
		return
	}
	for _, j := range s.jobs {
		s.report(fmt.Sprintf("cron %q: %s, next at %s", j.Name, j.Schedule, j.Due.Format(time.RFC3339)))
	}
	for {
		// Wake on the minute, because a crontab line has no finer granularity and a job asked for
		// 03:00 should not run at 03:00:47. Recomputed each time rather than a fixed ticker, so a
		// wake that comes back late does not walk the alignment forward.
		now := time.Now()
		timer := time.NewTimer(now.Truncate(time.Minute).Add(time.Minute).Sub(now))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			s.tickOnce(ctx)
		}
	}
}
