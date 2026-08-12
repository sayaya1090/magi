package main

import (
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/config"
	"github.com/sayaya1090/magi/internal/core/auth"
	"github.com/sayaya1090/magi/internal/core/session"
)

// What a companion does when nobody is watching, and changing it.
//
// # Why this is on the companion's own page
//
// A job belongs to one workspace: the daemon holding it is the only thing that runs it. It is not a
// property of the console or of the fleet, so it sits with that companion's other standing facts —
// beside its plan and its handoffs — rather than in a destination of its own. The layout says the
// same thing: past 840px there are no tabs, both panes are drawn, and a third tab would have been
// invisible on every desktop.
//
// # Why a person editing here is not a companion editing itself
//
// Same answer mcp.go gives about servers. A person at the console changing their own companion's
// schedule is the same act as opening the file in an editor, and it lands in the same place —
// <workspace>/.magi/config.toml, in git, readable and diffable. What is refused is a COMPANION
// accepting work from elsewhere on the network.
//
// # Telling the daemon
//
// The file is on a filesystem this process can see, so it writes it directly; but the ARMED set is
// in the daemon's memory, read from that file when it started. Without the reload call the daemon
// goes on firing yesterday's schedule while the page shows today's. It is best-effort: a daemon
// that went away between the write and the call is a reason to say the edit lands on its next
// start, not a reason to report that the edit failed.

// cronJob is one scheduled job as the page draws it.
type cronJob struct {
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	Prompt   string `json:"prompt"`
	Enabled  bool   `json:"enabled"`
	// Next is when it runs next, as an instant the page formats, and empty when it never will.
	Next string `json:"next,omitempty"`
	// Problem says why it can never run. A job in this state is the one a list MUST mark: it is
	// switched on, it looks ordinary, and nothing else will ever mention it again.
	Problem string `json:"problem,omitempty"`
	// File is where it is written, so a person can go and look.
	File string `json:"file"`
	// Global marks a job that comes from this machine's own config rather than the workspace's. It
	// can be switched off from here, but an edit always writes the project file — so the page has
	// to be able to say which is which.
	Global bool `json:"global,omitempty"`
}

func (s *server) cron(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.cronWrite(w, r)
		return
	}
	if s.forwarded(w, r, s.proxy) {
		return
	}
	in, err := s.target(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if in.Workdir == "" {
		writeJSON(w, "cron", []cronJob{})
		return
	}

	global, project := s.jobsFor(in.Workdir)
	projectFile := filepath.Join(in.Workdir, ".magi", "config.toml")
	globalFile := filepath.Join(s.cfgDir, "config.toml")

	out := make([]cronJob, 0, len(global)+len(project))
	for _, j := range app.DescribeJobs(config.MergeCron(global, project)) {
		_, fromProject := project[j.Name]
		row := cronJob{
			Name: j.Name, Schedule: j.Schedule, Prompt: j.Prompt, Enabled: j.Enabled,
			Problem: j.Problem, File: projectFile, Global: !fromProject,
		}
		if !fromProject {
			row.File = globalFile
		}
		if !j.Next.IsZero() {
			row.Next = j.Next.Format(time.RFC3339)
		}
		out = append(out, row)
	}
	writeJSON(w, "cron", out)
}

// jobsFor reads the two configs a workspace's schedule comes from.
func (s *server) jobsFor(workdir string) (global, project map[string]config.CronJob) {
	if g, err := config.Load(s.cfgDir); err == nil {
		global = g.Cron
	}
	if p, err := config.Load(filepath.Join(workdir, ".magi")); err == nil {
		project = p.Cron
	}
	return global, project
}

// cronWrite adds, changes, switches off or removes one job.
func (s *server) cronWrite(w http.ResponseWriter, r *http.Request) {
	if s.forwarded(w, r, s.proxy) || postOnly(w, r) {
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" || strings.ContainsAny(name, " \t[]\"'#.") {
		// The name becomes a TOML table header, so it has to be one. Refused rather than sanitised:
		// a name quietly rewritten is a job the person cannot find again in their own file.
		http.Error(w, "a job needs a name, and the name cannot contain spaces, quotes, "+
			"brackets, a hash or a dot", http.StatusBadRequest)
		return
	}
	in, err := s.target(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if in.Workdir == "" {
		http.Error(w, "that companion published no workspace, so there is no project config to write",
			http.StatusBadRequest)
		return
	}
	// A schedule is prompting, later. The route is `configure` because a job is configuration and
	// lives in a config file, and that is exactly how it would become the way around `prompt`: a
	// role written as "may set the model and the servers, may not give the companion work" would
	// hand somebody a form that gives it work every morning at nine. So both are asked for, and
	// the second one is asked HERE — after the companion is known, so the answer is about the
	// companion this job will actually run in.
	if who := s.whoFrom(r); !s.policy.Allows(who, auth.Prompt, nameOfSocket(r.URL.Query().Get("d")), "") {
		http.Error(w, refusal(who, auth.Prompt)+" — a scheduled job is a prompt with a clock on it",
			http.StatusForbidden)
		return
	}
	path := filepath.Join(in.Workdir, ".magi", "config.toml")
	section := "cron." + name

	if r.FormValue("delete") != "" {
		if err := config.RemoveSection(path, section); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		msg := "removed " + name + " from " + path
		// A job of the same name in the machine's config is still scheduled. Said, rather than
		// left to be discovered when it runs again tomorrow.
		if global, _ := s.jobsFor(in.Workdir); global[name].Schedule != "" {
			msg += ", but a job of that name is also in " + filepath.Join(s.cfgDir, "config.toml") +
				" and is still scheduled"
		}
		writeText(w, msg+s.tellDaemon(r))
		return
	}

	schedule := strings.TrimSpace(r.FormValue("schedule"))
	prompt := strings.TrimSpace(r.FormValue("prompt"))
	// An empty field means "leave that part alone", which is what makes switching a job off a
	// one-field post rather than a form that has to restate everything.
	existing := app.DescribeJobs(config.MergeCron(s.jobsFor(in.Workdir)))
	for _, j := range existing {
		if j.Name != name {
			continue
		}
		if schedule == "" {
			schedule = j.Schedule
		}
		if prompt == "" {
			prompt = j.Prompt
		}
	}
	if schedule == "" {
		http.Error(w, "a job needs a schedule, like \"0 9 * * 1-5\" or \"@daily\"", http.StatusBadRequest)
		return
	}
	if prompt == "" {
		http.Error(w, "a job needs a prompt — the thing to ask when it runs", http.StatusBadRequest)
		return
	}
	// Checked here rather than left for the daemon to reject at its next start. A schedule that
	// cannot run is worth refusing while the person who typed it is still looking at it.
	if why := scheduleProblem(schedule); why != "" {
		http.Error(w, why, http.StatusBadRequest)
		return
	}

	if err := config.SetKey(path, section, "schedule", schedule); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := config.SetKey(path, section, "prompt", prompt); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if v := r.FormValue("enabled"); v != "" {
		if err := config.SetRawKey(path, section, "enabled", fmt.Sprintf("%t", v == "true")); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	writeText(w, "wrote "+name+" to "+path+s.tellDaemon(r))
}

// scheduleProblem returns why a schedule cannot be used, or "".
func scheduleProblem(spec string) string {
	jobs := app.DescribeJobs(map[string]config.CronJob{"x": {Schedule: spec, Prompt: "x"}})
	if len(jobs) == 1 && jobs[0].Problem != "" {
		return jobs[0].Problem
	}
	return ""
}

// tellDaemon asks the running daemon to re-read the schedule it armed at startup, and says whether
// it heard.
//
// Not an error, and not silence either. The file is written and correct either way, so a daemon
// that is not running is not a failed edit — but it does mean the change takes effect at its next
// start rather than now, and somebody who just switched a job off is entitled to know which of
// those happened.
func (s *server) tellDaemon(r *http.Request) string {
	if err := s.withClient(r, func(cl *daemon.Client, _ session.SessionID) error {
		return cl.ReloadCron()
	}); err != nil {
		return " (the running daemon did not acknowledge it; it will pick this up when it next starts)"
	}
	return ""
}

func writeText(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if _, err := w.Write([]byte(msg)); err != nil {
		log.Printf("magi-web: answering a cron write: %v", err)
	}
}
