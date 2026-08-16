package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/config"
	"github.com/sayaya1090/magi/internal/core/cron"
	"github.com/sayaya1090/magi/internal/port"
)

// Letting the agent schedule its own work.
//
// # What this is, plainly
//
// One turn of a conversation can leave behind something that runs on its own, every morning, until
// somebody stops it. That is not like any other tool here: everything else does a thing and is
// finished. This is worth saying out loud in the code that implements it.
//
// The safeguard chosen for it is not a restriction on what can be scheduled. It is that the result
// is impossible to lose track of:
//
//   - a job is a named table in a config file a person can read, diff and revert
//   - the file is the project's, so it travels with the repo and shows up in review
//   - every surface lists the jobs, with when each next fires
//   - the daemon names them all at startup
//
// # Written to the project, not to the machine
//
// A job created here lands in <workspace>/.magi/config.toml. That is the committable one: whoever
// picks the repo up afterwards can see what it does unattended. Writing to the global config would
// put a standing job on one laptop where nobody else would ever find it, and a job nobody can find
// is the failure mode this whole design is arranged against.
//
// Jobs already in the GLOBAL config are listed and can be switched off from here, but an edit
// always writes the project file — the machine's own config is not something a conversation in one
// repository should be rewriting.

// scheduleFile is where an edit lands: the project's committable config.
func (a *App) scheduleFile(workdir string) string {
	return filepath.Join(workdir, ".magi", "config.toml")
}

// scheduleJobs reads the jobs this workspace has, machine-wide ones included.
func (a *App) scheduleJobs(workdir string) map[string]config.CronJob {
	var global map[string]config.CronJob
	if a.plat != nil {
		if g, err := config.Load(a.plat.ConfigDir()); err == nil {
			global = g.Cron
		}
	}
	var proj map[string]config.CronJob
	if p, err := config.Load(filepath.Join(workdir, ".magi")); err == nil {
		proj = p.Cron
	}
	return config.MergeCron(global, proj)
}

// EditSchedule lists or changes this workspace's unattended work.
//
// Every outcome is a sentence, including the refusals: the caller is a model deciding what to do
// next, and an error return would be surfaced as a tool failure rather than as the explanation of
// what was wrong with the schedule it wrote.
func (a *App) EditSchedule(workdir string, c port.ScheduleChange) (string, error) {
	switch strings.ToLower(strings.TrimSpace(c.Action)) {
	case "", "list":
		return a.renderSchedule(workdir), nil
	case "set":
		return a.setScheduled(workdir, c)
	case "remove", "delete":
		return a.removeScheduled(workdir, c)
	default:
		return fmt.Sprintf("%q is not something this can do. Use list, set, or remove.", c.Action), nil
	}
}

// badJobName reports why a name cannot be used, or "" when it can.
//
// Refused rather than sanitised, for the reason mcp.go gives about server names: the name becomes a
// TOML table header, and one quietly rewritten is a job the person cannot find again in their own
// file. A dot is refused too — [cron.a.b] is a nested table, not a job called "a.b".
func badJobName(name string) string {
	if name == "" {
		return "a job needs a name"
	}
	// config.BareName, the same allowlist every other header writer uses. This was the last writer
	// on the old denylist, which passed a newline — and this one is AGENT-facing (the schedule
	// tool), so a model-composed name with a stray control character bricked the config the same
	// way a crafted console request would have.
	if !config.BareName(name) {
		return fmt.Sprintf("%q cannot be a job name: letters, numbers, hyphens and underscores only "+
			"(the name becomes a TOML table header)", name)
	}
	return ""
}

func (a *App) setScheduled(workdir string, c port.ScheduleChange) (string, error) {
	name := strings.TrimSpace(c.Name)
	if why := badJobName(name); why != "" {
		return why, nil
	}
	existing, had := a.scheduleJobs(workdir)[name]

	schedule := strings.TrimSpace(c.Schedule)
	if schedule == "" {
		schedule = existing.Schedule // editing the words of a job without restating its time
	}
	if schedule == "" {
		return "a job needs a schedule, like \"0 9 * * 1-5\" (weekdays at nine) or \"@daily\"", nil
	}
	sch, err := cron.Parse(schedule)
	if err != nil {
		return fmt.Sprintf("that schedule will not do: %v", err), nil
	}
	next := sch.Next(time.Now())
	if next.IsZero() {
		return fmt.Sprintf("%q parses but never comes round, so nothing would ever run.", schedule), nil
	}

	prompt := strings.TrimSpace(c.Prompt)
	if prompt == "" {
		prompt = existing.Prompt
	}
	if prompt == "" {
		return "a job needs a prompt — the thing to ask when it runs", nil
	}

	path := a.scheduleFile(workdir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	section := "cron." + name
	if err := config.SetKey(path, section, "schedule", schedule); err != nil {
		return "", err
	}
	if err := config.SetKey(path, section, "prompt", prompt); err != nil {
		return "", err
	}
	// Only written when explicitly said. Absent means on, and writing "enabled = true" on every
	// edit would fill the file with a line that says what the default already says.
	if c.Enabled != nil {
		if err := config.SetRawKey(path, section, "enabled", fmt.Sprintf("%t", *c.Enabled)); err != nil {
			return "", err
		}
	}
	a.ReloadCron()

	verb := "Scheduled"
	if had {
		verb = "Changed"
	}
	out := fmt.Sprintf("%s %q: %s, next at %s. Written to %s.",
		verb, name, schedule, next.Format("2006-01-02 15:04 MST"), path)
	if c.Enabled != nil && !*c.Enabled {
		out += " It is switched off, so it will not run until that is changed."
	}
	return out, nil
}

func (a *App) removeScheduled(workdir string, c port.ScheduleChange) (string, error) {
	name := strings.TrimSpace(c.Name)
	if why := badJobName(name); why != "" {
		return why, nil
	}
	jobs := a.scheduleJobs(workdir)
	if _, ok := jobs[name]; !ok {
		return fmt.Sprintf("There is no job called %q. %s", name, a.renderSchedule(workdir)), nil
	}
	path := a.scheduleFile(workdir)
	if err := config.RemoveSection(path, "cron."+name); err != nil {
		return "", err
	}
	a.ReloadCron()
	// A job defined in the MACHINE config is still there after removing the project's copy of it.
	// Said rather than left to be discovered when it runs again tomorrow.
	if _, still := a.scheduleJobs(workdir)[name]; still {
		return fmt.Sprintf("Removed %q from %s, but a job of that name is also in this machine's "+
			"own config and is still scheduled. Edit that file to stop it.", name, path), nil
	}
	return fmt.Sprintf("Removed %q from %s. It will not run again.", name, path), nil
}

// ScheduledJobInfo is one job as the screens show it.
//
// Next and Problem are derived here rather than by each surface, because "when does this run next"
// is a question with one right answer and three places that ask it — the tool's listing, the
// terminal's editor, the console's tab. Three derivations would agree until one of them was fixed.
type ScheduledJobInfo struct {
	Name     string
	Schedule string
	Prompt   string
	Enabled  bool
	// Next is when it runs next. Zero when it never will — because it is switched off, or because
	// Problem says why.
	Next time.Time
	// Problem is why this job can never run, in words: an unparseable schedule, or one that parses
	// and matches no instant. Empty when there is nothing wrong. A job in this state is the one a
	// listing MUST mark, since nothing else will ever mention it again.
	Problem string
}

// ScheduledJobs returns this workspace's jobs, machine-wide ones included, in name order.
func (a *App) ScheduledJobs(workdir string) []ScheduledJobInfo {
	return DescribeJobs(a.scheduleJobs(workdir))
}

// DescribeJobs answers "when does each of these run next, and which of them never will".
//
// A function rather than a method, because the browser console is a different process with no App
// in it and asked the same question — and the answer being computed twice is how one of the two
// ends up saying a job runs tomorrow that in fact never runs at all.
func DescribeJobs(jobs map[string]config.CronJob) []ScheduledJobInfo {
	names := make([]string, 0, len(jobs))
	for n := range jobs {
		names = append(names, n)
	}
	sort.Strings(names)

	now := time.Now()
	out := make([]ScheduledJobInfo, 0, len(names))
	for _, n := range names {
		j := jobs[n]
		info := ScheduledJobInfo{Name: n, Schedule: j.Schedule, Prompt: j.Prompt, Enabled: j.On()}
		sch, err := cron.Parse(j.Schedule)
		switch {
		case err != nil:
			info.Problem = err.Error()
		case sch.Next(now).IsZero():
			info.Problem = "this schedule never comes round"
		case info.Enabled:
			info.Next = sch.Next(now)
		}
		out = append(out, info)
	}
	return out
}

// renderSchedule lists the jobs in lines. Built on ScheduledJobs so the words the model reads and
// the rows the screens draw cannot disagree about when something runs.
func (a *App) renderSchedule(workdir string) string {
	jobs := a.ScheduledJobs(workdir)
	if len(jobs) == 0 {
		return "Nothing is scheduled in this workspace."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d scheduled job(s) in this workspace:\n", len(jobs))
	for _, j := range jobs {
		fmt.Fprintf(&b, "\n%s · %s", j.Name, j.Schedule)
		switch {
		case j.Problem != "":
			fmt.Fprintf(&b, " · BROKEN (%s) — it will never run", j.Problem)
		case !j.Enabled:
			b.WriteString(" · off")
		default:
			fmt.Fprintf(&b, " · next %s", j.Next.Format("2006-01-02 15:04 MST"))
		}
		fmt.Fprintf(&b, "\n    %s", clipLine(oneLine(j.Prompt), 120))
	}
	b.WriteString("\n")
	return b.String()
}
