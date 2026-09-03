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

// cronCommandTimeout bounds one run of a scheduled command when the job names no bound of its own.
//
// The shell door's thirty seconds is right for a console — somebody is looking at it and there is
// no key to press to give up — and wrong for the thing this mode exists for, which is the nightly
// build. Ten minutes is long enough for that and short enough that a wedged command does not hold
// the workspace until morning; a job that needs longer says so.
const cronCommandTimeout = 10 * time.Minute

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
	// Command runs instead of asking, and Timeout bounds one run of it. Both empty for a prompt
	// job; Prompt empty for a command job. The two are never both set — armJobs refuses that.
	Command string
	Timeout time.Duration
	// Due is the next fire. Zero means never again — a schedule like "the 30th of February", which
	// parses cleanly and matches no instant.
	Due time.Time
}

type cronScheduler struct {
	app     *App
	workdir string
	now     func() time.Time
	report  func(string)
	// load reads the job definitions afresh. A function and not a snapshot: jobs are written to
	// config.toml, by a person or by the agent, while the daemon is running — and a daemon that
	// only read them at startup would need restarting before anything it was asked to schedule
	// could happen.
	load func() map[string]config.CronJob
	jobs []scheduledJob
	// last is the session each job most recently started, and the whole of the overlap check. There
	// is no separate "running" flag to keep in step with reality: whether that session is still
	// going is a question the App can already answer.
	last map[string]session.SessionID
	// said remembers which complaints have already been made, so a job with a typo in its schedule
	// is reported once rather than every minute for as long as the daemon lives.
	said map[string]string
}

// newCronScheduler builds a scheduler over a source of job definitions and arms what it finds.
func newCronScheduler(a *App, workdir string, load func() map[string]config.CronJob,
	now func() time.Time, report func(string)) *cronScheduler {

	s := &cronScheduler{app: a, workdir: workdir, now: now, report: report, load: load,
		last: map[string]session.SessionID{}, said: map[string]string{}}
	if report == nil {
		s.report = func(string) {}
	}
	s.reload()
	return s
}

// reload re-reads the definitions and brings the armed set into line with them.
//
// Called on every tick, which is how a job written while the daemon is running — by a person
// editing the file or by the agent scheduling its own follow-up — starts happening without a
// restart. Re-parsing a small TOML file once a minute costs nothing, and it means the FILE is the
// truth: there is no in-memory copy of the schedule that can drift away from what is on disk.
//
// What survives a reload is the arming. A job that is still there with the same schedule keeps the
// instant it is next owed, so editing one job does not silently push every other job's next run
// forward. A job whose schedule changed is re-armed against the new one, and a new job is armed
// from now — never from a slot in the past, for the same reason nothing catches up at startup.
func (s *cronScheduler) reload() {
	defs := map[string]config.CronJob{}
	if s.load != nil {
		defs = s.load()
	}
	prev := map[string]scheduledJob{}
	for _, j := range s.jobs {
		prev[j.Name] = j
	}

	// Sorted, so the order of firing within one minute is the same on every run and on every
	// daemon. Map order would make two identical companions disagree about which job went first.
	names := make([]string, 0, len(defs))
	for name := range defs {
		names = append(names, name)
	}
	sort.Strings(names)

	at := s.now()
	armed := make([]scheduledJob, 0, len(names))
	live := map[string]bool{}
	for _, name := range names {
		j := defs[name]
		if !j.On() {
			continue
		}
		// A job that cannot be used is reported and dropped, never silently kept as something that
		// will never fire: an unparseable schedule and a schedule of "never" look identical from
		// outside, and whoever mistyped one needs to hear about it rather than wonder in a week.
		// Reported once per distinct complaint — this runs every minute.
		asks, runs := strings.TrimSpace(j.Prompt), strings.TrimSpace(j.Command)
		if asks == "" && runs == "" {
			s.complain(name, "has neither a prompt nor a command — nothing to do, so it is not scheduled")
			continue
		}
		if asks != "" && runs != "" {
			// Refused rather than ordered. A job that both asks and runs has no stated answer for
			// which goes first, or whether the second happens when the first fails — and a rule
			// invented here would be a contract nobody wrote down.
			s.complain(name, "has both a prompt and a command — one or the other, not both")
			continue
		}
		bound := cronCommandTimeout
		if runs != "" && strings.TrimSpace(j.Timeout) != "" {
			d, terr := time.ParseDuration(strings.TrimSpace(j.Timeout))
			switch {
			case terr != nil:
				s.complain(name, fmt.Sprintf("timeout %q is not a duration (try \"10m\") — not scheduled", j.Timeout))
				continue
			case d <= 0:
				// Zero is not "no limit": an unbounded command holds the workspace's only turn
				// slot and every later firing is skipped behind it.
				s.complain(name, "timeout must be more than zero — a command with no bound holds the workspace")
				continue
			default:
				bound = d
			}
		}
		sch, err := cron.Parse(j.Schedule)
		if err != nil {
			s.complain(name, fmt.Sprintf("%v — not scheduled", err))
			continue
		}
		live[name] = true
		if p, ok := prev[name]; ok && p.Schedule.String() == sch.String() && !p.Due.IsZero() {
			// The words (or the command, or its bound) may have been edited; when it runs has not.
			p.Prompt, p.Command, p.Timeout = j.Prompt, j.Command, bound
			armed = append(armed, p)
			continue
		}
		due := sch.Next(at)
		if due.IsZero() {
			s.complain(name, fmt.Sprintf("%q never comes round — not scheduled", j.Schedule))
			continue
		}
		armed = append(armed, scheduledJob{Name: name, Schedule: sch, Prompt: j.Prompt,
			Command: j.Command, Timeout: bound, Due: due})
	}
	s.jobs = armed
	// Forget complaints about jobs that are no longer here, so fixing one and breaking it again
	// later says so again.
	for name := range s.said {
		if !live[name] && defs[name].Schedule == "" {
			delete(s.said, name)
		}
	}
}

// complain reports a problem with a job once, and again only if the problem changes.
func (s *cronScheduler) complain(name, what string) {
	if s.said[name] == what {
		return
	}
	s.said[name] = what
	s.report(fmt.Sprintf("cron %q: %s", name, what))
}

// Jobs returns what is armed, in name order.
//
// For tests. It said "for the daemon's startup line and for tests" and the startup line stopped
// calling it — a comment naming a caller that is gone is how a reader concludes the accessor is
// load-bearing and leaves it alone.
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

		if busy, ok := s.app.Running(); ok {
			// Anything, not just this job's own last run. Two jobs due in the same minute, or one
			// due while somebody is attached and working, are both ordinary — and this is a coding
			// agent, so two turns at once in one workspace are two writers to the same files with
			// nothing coordinating them. Naming the session is what makes the skip diagnosable.
			s.report(fmt.Sprintf("cron %q: skipped, %s is still going in this workspace",
				j.Name, busy))
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

// fire opens a session for one run of a job — and then either asks the companion the job's prompt,
// verbatim, or runs its command and writes down what happened.
//
// Both make a session, which is the record: SessionMeta.Origin already says which job it was, so a
// command run is listed, read and traced exactly like a prompt run and nothing new has to be kept.
// A command run never calls the model, which is the whole of why the mode exists.
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
	if strings.TrimSpace(j.Command) != "" {
		return sid, s.run(ctx, sid, actor, j)
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

// run carries out a command job and writes the command, its output and its exit into the session.
//
// Written down whichever way it went. A command that failed at 3am is the reason somebody opens
// this session in the morning, and a run that left no trace would be indistinguishable from one
// that never fired — the thing the schedule's own "skipped, with the reason recorded" rule exists
// to prevent one level up.
func (s *cronScheduler) run(ctx context.Context, sid session.SessionID, actor event.Actor, j scheduledJob) error {
	rctx, cancel := context.WithTimeout(ctx, j.Timeout)
	defer cancel()
	out, code, err := s.app.RunShell(rctx, s.workdir, j.Command)
	var b strings.Builder
	fmt.Fprintf(&b, "$ %s\n", j.Command)
	if err != nil {
		fmt.Fprintf(&b, "\n%v", err)
		if rctx.Err() != nil {
			// The bound, not the command, ended this — said plainly, because "it produced nothing"
			// and "it was cut off" lead to different next steps.
			fmt.Fprintf(&b, " (stopped after %s)", j.Timeout)
		}
	} else {
		fmt.Fprintf(&b, "\nexit %d\n\n%s", code, out)
	}
	// WithoutCancel: the bound above may be the very thing that just fired, and the record of what
	// happened is worth more than the context it happened under.
	if aerr := s.app.AppendCronRun(context.WithoutCancel(ctx), sid, actor, b.String()); aerr != nil {
		return fmt.Errorf("session %s ran the command but the record did not land: %w", sid, aerr)
	}
	return err
}

// Running names a session with a turn in flight, if there is one.
//
// The overlap check, and it is about the WORKSPACE rather than about one job. It used to ask only
// whether that job's own last run was still going, which left the two cases that actually happen:
// two jobs due in the same minute, and a job due while somebody is attached and working. An agent
// that edits files cannot have two turns at once in one tree — there is nothing coordinating them.
//
// It reads the App's own state rather than a flag a caller keeps: startRun sets cancel before it
// returns and the teardown clears it, so the answer is already maintained by the thing that knows.
// A second copy would be a second thing to keep true.
//
// Every session this App holds is in its workspace — a daemon serves one — so "any" and "in this
// workspace" are the same set.
func (a *App) Running() (session.SessionID, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for sid, st := range a.states {
		if st != nil && st.cancel != nil {
			return sid, true
		}
	}
	return "", false
}

// WritingRun is Running for a caller that has to protect the WORKSPACE rather than the process.
//
// Two turns in one workspace can corrupt each other's files, and the guard that records what a turn
// changed reads each file before and after an edit — which is only race-free when the writes are
// serialised. That is the whole reason handed-over work waits for a turn to finish.
//
// A turn that cannot write is not part of that reason. A session opened for a question — the
// looking role, whose tools are read, glob, grep and list and are enforced as such — has nothing to
// serialise against, so waiting for it is waiting for nothing. Meeting turns are already outside
// this: they never enter the run states at all.
func (a *App) WritingRun() (session.SessionID, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for sid, st := range a.states {
		if st == nil || st.cancel == nil {
			continue
		}
		if m, ok := a.metaLocked(sid); ok && m.Agent == LookingAgent {
			continue
		}
		return sid, true
	}
	return "", false
}

// ReloadCron tells a running scheduler that the job definitions have changed.
//
// Push, not poll. Jobs are written by the schedule tool, which runs inside this process, so the
// thing that changed the file can simply say so — and a daemon that instead re-read config.toml
// every minute would be burning a parse a minute to discover a change it already knew about, and
// would still take up to a minute to act on it.
//
// A no-op when no scheduler is running (not a daemon), and non-blocking: the caller is a tool call
// answering a model, and it must not wait on a scheduler that is mid-fire.
func (a *App) ReloadCron() {
	a.mu.Lock()
	ch := a.cronReload
	a.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default: // a reload is already pending; it will pick up this change too
	}
}

// RunCron fires this workspace's scheduled jobs until ctx is done. It blocks; the daemon runs it in
// a goroutine.
//
// It keeps running even with nothing armed, because the schedule tool can arm something later and a
// loop that had already returned would leave that job never firing. The cost of waiting is one
// timer.
func (a *App) RunCron(ctx context.Context, workdir string, load func() map[string]config.CronJob, report func(string)) {
	reload := make(chan struct{}, 1)
	a.mu.Lock()
	a.cronReload = reload
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.cronReload = nil
		a.mu.Unlock()
	}()

	s := newCronScheduler(a, workdir, load, time.Now, report)
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
		case <-reload:
			timer.Stop()
			s.reload()
		case <-timer.C:
			s.tickOnce(ctx)
		}
	}
}
